### mcldsp
Migrate a c-lightning database from SQLite to Postgres!

## Why migrate

Because Postgres is very good, and it can replicate writes instantly with instances elsewhere, so your channels are safer.

# Beware: this may destroy your life and make you lose all your money, use at your own risk.

## How to use

1. Compile this with `go get` or `go build`, or download a binary from [releases](releases);
2. Create a database on Postgres;
3. Stop your c-lightning daemon: `lightning-cli stop`;
4. Run `mcldsp -sqlite=/home/user/.lightning/bitcoin/lightningd.sqlite3 -lightningd=(which lightningd) -postgres=postgres:///myclightningdatabase` (replace with your actual values);
5. Change your `~/.lightning/config` file, add a `wallet=postgres:///myclightningdatabase` there so the next time it starts it will use the PostgreSQL database and not the SQLite file.
6. Start `lightningd` again.

### Now you're ready!

If you want to setup instant replication go over to https://www.postgresql.org/docs/current/high-availability.html
