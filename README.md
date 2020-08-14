### mcldsp
Migrate a c-lightning database from SQLite to Postgres!

## Why migrate

Because Postgres is very good, it supports remote access, simultaneous readers and writers, it can replicate writes with instances elsewhere, so your channels are safer. Everything is better.

# Beware: this may destroy your life and make you lose all your money, use at your own risk.

## How to use

1. Download the [release](https://github.com/fiatjaf/mcldsp/releases) corresponding to the database version of your `lightningd.sqlite3` file. To find out what is your version, run `sqlite3 ~/.lightning/bitcoin/lightningd.sqlite3 'select version from version'`. If there's no release corresponding to your version, upgrade to the newest `master` and [ping me](https://t.me/fiatjaf) so I can make a new release with the latest changes.
2. Your c-lightning should be compiled with support for PostgreSQL. That happens automatically if you have libpq installed and reachable at `/usr/include/postgresql/libpq-fe.h`.
3. Create a database on Postgres.
4. Stop your c-lightning daemon: `lightning-cli stop`.
5. Run `mcldsp -sqlite=/home/user/.lightning/bitcoin/lightningd.sqlite3 -lightningd=(which lightningd) -postgres=postgres:///myclightningdatabase` (replace with your actual values).
6. Change your `~/.lightning/config` file, add a `wallet=postgres:///myclightningdatabase` there so the next time it starts it will use the PostgreSQL database and not the SQLite file.
7. Start `lightningd` again and check if everything works.
8. Delete `mcldsp` so you never run it again.
9. Delete your `lightningd.sqlite` file so you don't try to use it again.

### Now you're ready!

If you want to setup replication go over to https://github.com/gabridome/docs/blob/master/c-lightning_with_postgresql_reliability.md
