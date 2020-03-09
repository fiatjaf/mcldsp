### mcldsp
Migrate a c-lightning database from SQLite to Postgres!

## Why migrate

Because Postgres is very good, it supports remote access, simultaneous readers and writers, it can replicate writes with instances elsewhere, so your channels are safer. Everything is better.

# Beware: this may destroy your life and make you lose all your money, use at your own risk.

## How to use

1. Your `lightningd.sqlite3` database should be at version `131`, which corresponds more-or-less to commit `53913c5119bce660b4a20ba394f4a3ea7179243c`. To check that you can run `sqlite3 lightningd.sqlite3 'select version from version'`. If you are behind, upgrade your c-lightning installation and run it first, so it can migrate to version 131.
2. Your c-lightning should be compiled with support for PostgreSQL. That happens automatically if you have libpq installed and reachable at `/usr/include/postgresql/libpq-fe.h`.
3. Compile `mcldsp` with `go get` or `go build`, or download a binary from [releases](releases).
4. Create a database on Postgres.
5. Stop your c-lightning daemon: `lightning-cli stop`.
6. Run `mcldsp -sqlite=/home/user/.lightning/bitcoin/lightningd.sqlite3 -lightningd=(which lightningd) -postgres=postgres:///myclightningdatabase` (replace with your actual values).
7. Change your `~/.lightning/config` file, add a `wallet=postgres:///myclightningdatabase` there so the next time it starts it will use the PostgreSQL database and not the SQLite file.
8. Move your `lightningd.sqlite` file to somewhere else just so you are not confused in the future.
9. Start `lightningd` again.
10. Delete `mcldsp` so you never run it again.

### Now you're ready!

If you want to setup replication go over to https://www.postgresql.org/docs/current/high-availability.html
