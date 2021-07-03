package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

const version = 162
const USAGE = `
mcldsp

Migrate your c-lightning database from SQLite to Postgres

Usage:
  mcldsp -sqlite=<sqlite_file> -postgres=<postgres_dsn> -lightningd=<lightningd_executable>
`

var sqlt *sqlx.DB
var lite *sqlx.Tx
var pg *sqlx.DB
var err error

func main() {
	sqlite := flag.String("sqlite", "", "Path to the lightningd.sqlite3 file.")
	postgres := flag.String("postgres", "", "Postgres address like postgres://...")
	lightningd := flag.String("lightningd", "", "Path to the lightningd executable.")
	flag.Parse()

	if *sqlite == "" || *postgres == "" || *lightningd == "" {
		fmt.Println(strings.TrimSpace(USAGE))
		return
	}

	fmt.Println("  > connecting to sqlite and postgres.")

	sqlt, err = sqlx.Connect("sqlite3", *sqlite)
	if err != nil {
		fmt.Println("sqlite connection error", err)
		return
	}
	lite = sqlt.MustBegin()
	defer lite.Rollback()

	pg, err = sqlx.Connect("postgres", *postgres)
	if err != nil {
		fmt.Println("postgres connection error", err)
		return
	}

	// check if database structure is in place
	var tablecount int
	err = pg.Get(&tablecount, "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'")
	if tablecount == 0 {
		fmt.Println("  > starting lightningd so it will create the needed postgres tables.")

		// if not, create database structure
		lightningd := *lightningd
		cmd := exec.Command(lightningd,
			"--lightning-dir=/tmp/mcldsp-lightning",
			"--network=regtest",
			"--wallet="+*postgres,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Start()
		time.Sleep(time.Second * 35)
		cmd.Process.Kill()

		fmt.Println("  > database schema created.")
	} else {
		fmt.Println("  > database schema was already created.")
	}

	// check tables are created
	var expectedTableCount int
	var createdTableCount int
	lite.Get(&expectedTableCount, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name != 'android_metadata' AND name != 'sqlite_sequence'")
	pg.Get(&createdTableCount, "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'")
	if expectedTableCount != createdTableCount || createdTableCount < 18 {
		fmt.Printf("postgres database structure wasn't created correctly: %v (expected %d tables to be created, got %d)\n", err, expectedTableCount, createdTableCount)
		return
	}

	// check htlc_sigs is empty
	var chtlcsigns int
	err = lite.Get(&chtlcsigns, "SELECT count(*) FROM htlc_sigs")
	if err != nil || chtlcsigns != 0 {
		fmt.Println("htlc_sigs table is not empty", err)
		return
	}

	// check version
	fmt.Println("  > checking if database versions are correct.")

	var dbversionlite int
	var dbversionpg int
	err1 := lite.Get(&dbversionlite, "SELECT version FROM version")
	err2 := pg.Get(&dbversionpg, "SELECT version FROM version")
	if err1 != nil || err2 != nil {
		fmt.Println("error fetching db versions", err)
		return
	}
	if dbversionlite != dbversionpg || dbversionpg != version {
		fmt.Printf("db versions mismatch. expected %d, got sqlite:%d, postgres:%d\n", version, dbversionlite, dbversionpg)
		return
	}

	// start updating on a big transaction
	fmt.Println("  > moving data from sqlite to postgres in a big db transaction.")

	pgx, err := pg.Beginx()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer pgx.Rollback()

	// update vars
	var vars []struct {
		Name    sql.NullString `db:"name"`
		Val     sql.NullString `db:"val"`
		Intval  sql.NullInt64  `db:"intval"`
		Blobval sqlblob        `db:"blobval"`
	}
	err = lite.Select(&vars, `SELECT * FROM vars`)
	if err != nil {
		fmt.Println("error selecting vars", err)
		return
	}
	for _, v := range vars {
		if v.Name.String == "genesis_hash" {
			// apparently old versions stored a blob in the 'val' column, but this is
			// no longer needed nor supported in postgres.
			v.Val = sql.NullString{Valid: false}
		}

		_, err := pgx.NamedExec(`
INSERT INTO vars
VALUES (:name, :val, :intval, :blobval)
ON CONFLICT (name) DO UPDATE SET val=:val, intval=:intval, blobval=:blobval
            `, v)
		if err != nil {
			fmt.Println(v, "error inserting var", err)
			return
		}
	}

	// fix sqlite's invoices.features if there are wrong fields
	lite.Exec(`UPDATE invoices SET features = '' WHERE length(features) = 0`)

	// update all the other tables except version and db_upgrades
	if err := copyRows(pgx, "blocks", struct {
		Height   sql.NullInt64 `db:"height"`
		Hash     sqlblob       `db:"hash"`
		PrevHash sqlblob       `db:"prev_hash"`
	}{}, "height"); err != nil {
		return
	}

	if err := copyRows(pgx, "channel_configs", struct {
		Id                   sql.NullInt64 `db:"id"`
		DustLimit            sql.NullInt64 `db:"dust_limit_satoshis"`
		MaxHTLCValueInFlight sql.NullInt64 `db:"max_htlc_value_in_flight_msat"`
		ChannelReserve       sql.NullInt64 `db:"channel_reserve_satoshis"`
		HTLCMinimum          sql.NullInt64 `db:"htlc_minimum_msat"`
		ToSelfDelay          sql.NullInt64 `db:"to_self_delay"`
		MaxAcceptedHTLCs     sql.NullInt64 `db:"max_accepted_htlcs"`
	}{}, "id"); err != nil {
		return
	}

	if err := copyRows(pgx, "peers", struct {
		Id      int64   `db:"id"`
		NodeId  sqlblob `db:"node_id"`
		Address string  `db:"address"`
	}{}, "id"); err != nil {
		return
	}

	if err := copyRows(pgx, "channels", struct {
		Id                            int64          `db:"id"`
		PeerId                        sql.NullInt64  `db:"peer_id"`
		ShortChannelId                sql.NullString `db:"short_channel_id"`
		ChannelConfigLocal            int64          `db:"channel_config_local"`
		ChannelConfigRemote           int64          `db:"channel_config_remote"`
		State                         int64          `db:"state"`
		Funder                        int64          `db:"funder"`
		ChannelFlags                  int64          `db:"channel_flags"`
		MinimumDepth                  int64          `db:"minimum_depth"`
		NextIndexLocal                int64          `db:"next_index_local"`
		NextIndexRemote               int64          `db:"next_index_remote"`
		NextHtlcId                    int64          `db:"next_htlc_id"`
		FundingTxId                   sqlblob        `db:"funding_tx_id"`
		FundingTxOutnum               int64          `db:"funding_tx_outnum"`
		FundingSatoshi                int64          `db:"funding_satoshi"`
		FundingTxRemoteSigsReceived   int64          `db:"funding_tx_remote_sigs_received"`
		OurFundingSatoshi             int64          `db:"our_funding_satoshi"`
		FundingLockedRemote           int64          `db:"funding_locked_remote"`
		PushMsatoshi                  int64          `db:"push_msatoshi"`
		MsatoshiLocal                 int64          `db:"msatoshi_local"`
		FundingkeyRemote              sqlblob        `db:"fundingkey_remote"`
		RevocationBasepointRemote     sqlblob        `db:"revocation_basepoint_remote"`
		PaymentBasepointRemote        sqlblob        `db:"payment_basepoint_remote"`
		HtlcBasepointRemote           sqlblob        `db:"htlc_basepoint_remote"`
		DelayedPaymentBasepointRemote sqlblob        `db:"delayed_payment_basepoint_remote"`
		PerCommitRemote               sqlblob        `db:"per_commit_remote"`
		OldPerCommitRemote            sqlblob        `db:"old_per_commit_remote"`
		LocalFeeratePerKw             sql.NullInt64  `db:"local_feerate_per_kw"`
		RemoteFeeratePerKw            sql.NullInt64  `db:"remote_feerate_per_kw"`
		ShachainRemoteId              int64          `db:"shachain_remote_id"`
		ShutdownScriptPubKeyRemote    sqlblob        `db:"shutdown_scriptpubkey_remote"`
		ShutdownKeyidxLocal           int64          `db:"shutdown_keyidx_local"`
		LastSentCommitState           sql.NullInt64  `db:"last_sent_commit_state"`
		LastSentCommitId              sql.NullInt64  `db:"last_sent_commit_id"`
		LastTx                        sqlblob        `db:"last_tx"`
		LastSig                       sqlblob        `db:"last_sig"`
		ClosingFeeReceived            sql.NullInt64  `db:"closing_fee_received"`
		ClosingSigReceived            sqlblob        `db:"closing_sig_received"`
		FirstBlocknum                 int64          `db:"first_blocknum"`
		LastWasRevoke                 int64          `db:"last_was_revoke"`
		InPaymentsOffered             sql.NullInt64  `db:"in_payments_offered"`
		InPaymentsFulfilled           sql.NullInt64  `db:"in_payments_fulfilled"`
		InMsatoshiOffered             sql.NullInt64  `db:"in_msatoshi_offered"`
		InMsatoshiFulfilled           sql.NullInt64  `db:"in_msatoshi_fulfilled"`
		OutPaymentsOffered            sql.NullInt64  `db:"out_payments_offered"`
		OutPaymentsFulfilled          sql.NullInt64  `db:"out_payments_fulfilled"`
		OutMsatoshiOffered            sql.NullInt64  `db:"out_msatoshi_offered"`
		OutMsatoshiFulfilled          sql.NullInt64  `db:"out_msatoshi_fulfilled"`
		MinPossibleFeerate            int64          `db:"min_possible_feerate"`
		MaxPossibleFeerate            int64          `db:"max_possible_feerate"`
		MsatoshiToUsMin               int64          `db:"msatoshi_to_us_min"`
		MsatoshiToUsMax               int64          `db:"msatoshi_to_us_max"`
		FuturePerCommitmentPoint      sqlblob        `db:"future_per_commitment_point"`
		LastSentCommit                sqlblob        `db:"last_sent_commit"`
		FeerateBase                   int64          `db:"feerate_base"`
		FeeratePpm                    int64          `db:"feerate_ppm"`
		RemoteUpfrontShutdownScript   sqlblob        `db:"remote_upfront_shutdown_script"`
		RemoteAnnNodeSig              sqlblob        `db:"remote_ann_node_sig"`
		RemoteAnnBitcoinSig           sqlblob        `db:"remote_ann_bitcoin_sig"`
		OptionStaticRemotekey         int64          `db:"option_static_remotekey"`
		ShutdownScriptPubKeyLocal     sqlblob        `db:"shutdown_scriptpubkey_local"`
		OptionAnchorOutputs           sql.NullInt64  `db:"option_anchor_outputs"`
		FullChannelId                 sqlblob        `db:"full_channel_id"`
		FundingPSBT                   sqlblob        `db:"funding_psbt"`
		Closer                        int64          `db:"closer"`
		StateChangeReason             int64          `db:"state_change_reason"`
		RevocationBasepointLocal      sqlblob        `db:"revocation_basepoint_local"`
		PaymentBasepointLocal         sqlblob        `db:"payment_basepoint_local"`
		HTLCBasepointLocal            sqlblob        `db:"htlc_basepoint_local"`
		DelayedPaymentBasepointLocal  sqlblob        `db:"delayed_payment_basepoint_local"`
		FundingPubkeyLocal            sqlblob        `db:"funding_pubkey_local"`
		ShutdownWrongTxid             sqlblob        `db:"shutdown_wrong_txid"`
		ShutdownWrongOutnum           int            `db:"shutdown_wrong_outnum"`
		LocalStaticRemotekeyStart     int64          `db:"local_static_remotekey_start"`
		RemoteStaticRemotekeyStart    int64          `db:"remote_static_remotekey_start"`
	}{}, "id"); err != nil {
		return
	}

	if err := copyRows(pgx, "channel_feerates", struct {
		ChannelId    int64 `db:"channel_id"`
		HState       int64 `db:"hstate"`
		FeeRatePerKw int64 `db:"feerate_per_kw"`
	}{}, "channel_id, hstate"); err != nil {
		return
	}

	if err := copyRows(pgx, "channel_htlcs", struct {
		Id             int64         `db:"id"`
		ChannelId      int64         `db:"channel_id"`
		ChannelHTLCId  int64         `db:"channel_htlc_id"`
		Direction      int64         `db:"direction"`
		OriginHTLC     sql.NullInt64 `db:"origin_htlc"`
		MSatoshi       int64         `db:"msatoshi"`
		CLTVExpiry     int64         `db:"cltv_expiry"`
		PaymentHash    sqlblob       `db:"payment_hash"`
		PaymentKey     sqlblob       `db:"payment_key"`
		RoutingOnion   sqlblob       `db:"routing_onion"`
		FailureMessage sqlblob       `db:"failuremsg"`
		MalformedOnion sql.NullInt64 `db:"malformed_onion"`
		HState         int64         `db:"hstate"`
		SharedSecret   sqlblob       `db:"shared_secret"`
		ReceivedTime   sql.NullInt64 `db:"received_time"`
		LocalFailMsg   sqlblob       `db:"localfailmsg"`
		PartId         sql.NullInt64 `db:"partid"`
		WeFilled       sql.NullInt64 `db:"we_filled"`
	}{}, "id"); err != nil {
		return
	}

	if err := copyRows(pgx, "transactions", struct {
		Id          sqlblob       `db:"id"`
		Blockheight sql.NullInt64 `db:"blockheight"`
		Txindex     sql.NullInt64 `db:"txindex"`
		Rawtx       sqlblob       `db:"rawtx"`
		Type        sql.NullInt64 `db:"type"`
		ChannelId   sql.NullInt64 `db:"channel_id"`
	}{}, "id"); err != nil {
		return
	}

	if err := copyRows(pgx, "transaction_annotations", struct {
		TxId     sqlblob       `db:"txid"`
		Idx      int64         `db:"idx"`
		Location int64         `db:"location"`
		Type     int64         `db:"type"`
		Channel  sql.NullInt64 `db:"channel"`
	}{}, "txid, idx"); err != nil {
		return
	}

	if err := copyRows(pgx, "channeltxs", struct {
		Id            int64   `db:"id"`
		ChannelId     int64   `db:"channel_id"`
		Type          int64   `db:"type"`
		TransactionId sqlblob `db:"transaction_id"`
		InputNum      int64   `db:"input_num"`
		Blockheight   int64   `db:"blockheight"`
	}{}, "id"); err != nil {
		return
	}

	if err := copyRows(pgx, "outputs", struct {
		PrevOutTx           sqlblob       `db:"prev_out_tx"`
		PrevOutIndex        int64         `db:"prev_out_index"`
		Value               int64         `db:"value"`
		Type                int64         `db:"type"`
		Status              int64         `db:"status"`
		Keyindex            int64         `db:"keyindex"`
		ChannelId           sql.NullInt64 `db:"channel_id"`
		PeerId              sqlblob       `db:"peer_id"`
		CommitmentPoint     sqlblob       `db:"commitment_point"`
		ConfirmationHeight  sql.NullInt64 `db:"confirmation_height"`
		SpendHeight         sql.NullInt64 `db:"spend_height"`
		ScriptPubKey        sqlblob       `db:"scriptpubkey"`
		ReservedTil         sql.NullInt64 `db:"reserved_til"`
		OptionAnchorOutputs sql.NullInt64 `db:"option_anchor_outputs"`
	}{}, "prev_out_tx, prev_out_index"); err != nil {
		return
	}

	if err := copyRows(pgx, "payments", struct {
		Id              int64          `db:"id"`
		Timestamp       int64          `db:"timestamp"`
		Status          int64          `db:"status"`
		PaymentHash     sqlblob        `db:"payment_hash"`
		Destination     sqlblob        `db:"destination"`
		Msatoshi        int64          `db:"msatoshi"`
		PaymentPreimage sqlblob        `db:"payment_preimage"`
		PathSecrets     sqlblob        `db:"path_secrets"`
		RouteNodes      sqlblob        `db:"route_nodes"`
		RouteChannels   sqlblob        `db:"route_channels"`
		Failonionreply  sqlblob        `db:"failonionreply"`
		Faildestperm    sql.NullInt64  `db:"faildestperm"`
		Failindex       sql.NullInt64  `db:"failindex"`
		Failcode        sql.NullInt64  `db:"failcode"`
		Failnode        sqlblob        `db:"failnode"`
		Failchannel     sql.NullString `db:"failchannel"`
		Failupdate      sqlblob        `db:"failupdate"`
		MsatoshiSent    int64          `db:"msatoshi_sent"`
		Faildetail      sql.NullString `db:"faildetail"`
		Description     sql.NullString `db:"description"`
		Faildirection   sql.NullInt64  `db:"faildirection"`
		Bolt11          sql.NullString `db:"bolt11"`
		TotalMsat       int64          `db:"total_msat"`
		PartId          int64          `db:"partid"`
		LocalOfferId    sqlblob        `db:"local_offer_id"`
	}{}, "payment_hash, partid"); err != nil {
		return
	}

	if err := copyRows(pgx, "invoices", struct {
		Id               int64          `db:"id"`
		State            int64          `db:"state"`
		Msatoshi         sql.NullInt64  `db:"msatoshi"`
		PaymentHash      sqlblob        `db:"payment_hash"`
		PaymentKey       sqlblob        `db:"payment_key"`
		Label            string         `db:"label"`
		ExpiryTime       int64          `db:"expiry_time"`
		PayIndex         sql.NullInt64  `db:"pay_index"`
		MsatoshiReceived sql.NullInt64  `db:"msatoshi_received"`
		PaidTimestamp    sql.NullInt64  `db:"paid_timestamp"`
		Bolt11           string         `db:"bolt11"`
		Description      sql.NullString `db:"description"`
		Features         sqlblob        `db:"features"`
		LocalOfferId     sqlblob        `db:"local_offer_id"`
	}{}, "id"); err != nil {
		return
	}

	if err := copyRows(pgx, "forwarded_payments", struct {
		InHtlcId       sql.NullInt64 `db:"in_htlc_id"`
		OutHtlcId      sql.NullInt64 `db:"out_htlc_id"`
		InChannelScid  int64         `db:"in_channel_scid"`
		OutChannelScid sql.NullInt64 `db:"out_channel_scid"`
		InMsatoshi     int64         `db:"in_msatoshi"`
		OutMsatoshi    sql.NullInt64 `db:"out_msatoshi"`
		State          int64         `db:"state"`
		ReceivedTime   int64         `db:"received_time"`
		ResolvedTime   sql.NullInt64 `db:"resolved_time"`
		Failcode       sql.NullInt64 `db:"failcode"`
	}{}, "in_htlc_id, out_htlc_id"); err != nil {
		return
	}

	if err := copyRows(pgx, "shachains", struct {
		Id       int64 `db:"id"`
		MinIndex int64 `db:"min_index"`
		NumValid int64 `db:"num_valid"`
	}{}, "id"); err != nil {
		return
	}

	if err := copyRows(pgx, "shachain_known", struct {
		ShachainId int64   `db:"shachain_id"`
		Pos        int64   `db:"pos"`
		Idx        int64   `db:"idx"`
		Hash       sqlblob `db:"hash"`
	}{}, "shachain_id, pos"); err != nil {
		return
	}

	if err := copyRows(pgx, "utxoset", struct {
		Txid         sqlblob       `db:"txid"`
		Outnum       int64         `db:"outnum"`
		Blockheight  int64         `db:"blockheight"`
		Spendheight  sql.NullInt64 `db:"spendheight"`
		Txindex      int64         `db:"txindex"`
		ScriptPubKey sqlblob       `db:"scriptpubkey"`
		Satoshis     int64         `db:"satoshis"`
	}{}, "txid, outnum"); err != nil {
		return
	}

	if err := copyRows(pgx, "penalty_bases", struct {
		ChannelId int64   `db:"channel_id"`
		CommitNum int64   `db:"commitnum"`
		Txid      sqlblob `db:"txid"`
		OutNum    int     `db:"outnum"`
		Amount    int64   `db:"amount"`
	}{}, "channel_id, commitnum"); err != nil {
		return
	}

	if err := copyRows(pgx, "channel_state_changes", struct {
		ChannelId int64  `db:"channel_id"`
		Timestamp int64  `db:"timestamp"`
		OldState  int64  `db:"old_state"`
		NewState  int64  `db:"new_state"`
		Cause     int64  `db:"cause"`
		Message   string `db:"message"`
	}{}, ""); err != nil {
		return
	}

	if err := copyRows(pgx, "offers", struct {
		OfferId sqlblob `db:"offer_id"`
		Bolt12  string  `db:"bolt12"`
		Label   string  `db:"label"`
		Status  int64   `db:"status"`
	}{}, "offer_id"); err != nil {
		return
	}

	if err := copyRows(pgx, "channel_funding_inflights", struct {
		ChannelId                   int64   `db:"channel_id"`
		FundingTxId                 sqlblob `db:"funding_tx_id"`
		FundingTxOutnum             int     `db:"funding_tx_outnum"`
		FundingFeerate              int     `db:"funding_feerate"`
		FundingSatoshi              int64   `db:"funding_satoshi"`
		OurFundingSatoshi           int64   `db:"our_funding_satoshi"`
		FundingPSBT                 sqlblob `db:"funding_psbt"`
		LastTx                      sqlblob `db:"last_tx"`
		LastSig                     sqlblob `db:"last_sig"`
		FundingTxRemoteSigsReceived int     `db:"funding_tx_remote_sigs_received"`
	}{}, "channel_id, funding_tx_id"); err != nil {
		return
	}

	// update sequences
	var version string
	if err := pg.Get(&version, "SELECT version()"); err != nil {
		fmt.Println("failed to get database version")
	} else {
		if strings.Index(version, "CockroachDB") != -1 {
			// skip this part in cockroach
		} else {
			if err := setSequence("channel_configs_id_seq"); err != nil {
				return
			}
			if err := setSequence("channel_htlcs_id_seq"); err != nil {
				return
			}
			if err := setSequence("channels_id_seq"); err != nil {
				return
			}
			if err := setSequence("channeltxs_id_seq"); err != nil {
				return
			}
			if err := setSequence("invoices_id_seq"); err != nil {
				return
			}
			if err := setSequence("payments_id_seq"); err != nil {
				return
			}
			if err := setSequence("peers_id_seq"); err != nil {
				return
			}
			if err := setSequence("shachains_id_seq"); err != nil {
				return
			}
		}
	}

	// end it
	err = pgx.Commit()
	if err != nil {
		fmt.Println("error on final commit", err)
		return
	}

	fmt.Println("  > all data moved. you should now stop using sqlite and use postgres only.")
}
