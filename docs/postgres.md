# Running dcrpool with PostgreSQL

Tested with PostgreSQL 13.0, 14.9, 15.4, and 16.0.

**Note:** When running in Postgres mode, backups will not be created
automatically by dcrpool.

## Setup

1. Connect to your instance of PostgreSQL using `psql` to create a new role and
    a new database for dcrpool.
    Be sure to substitute the example password `12345` with something more secure.

    ```no-highlight
    postgres=# CREATE ROLE dcrpooluser WITH LOGIN NOINHERIT PASSWORD '12345';
    CREATE ROLE
    postgres=# CREATE DATABASE dcrpooldb OWNER dcrpooluser;
    CREATE DATABASE
    ```

1. **Developers only** - if you are modifying code and wish to run the dcrpool
   test suite, you will need to create an additional database.

    ```no-highlight
    postgres=# CREATE DATABASE dcrpooltestdb OWNER dcrpooluser;
    CREATE DATABASE
    ```

    Alternatively the test suite can be run against a database created in a Docker container using the following one-liner:

    ```no-highlight
    docker run -d --rm -e POSTGRES_USER=dcrpooluser -e POSTGRES_PASSWORD=12345 -e POSTGRES_DB=dcrpooltestdb -p 5432:5432 postgres:16.0
    ```

1. Add the database connection details to the dcrpool config file.

    ```no-highlight
    postgres=true
    postgreshost=127.0.0.1
    postgresport=5432
    postgresuser=dcrpooluser
    postgrespass=12345
    postgresdbname=dcrpooldb
    ```

## Tuning

A helpful online tool to determine good settings for your system is called
[PGTune](https://pgtune.leopard.in.ua/#/). After providing basic information
about your hardware, PGTune will output a snippet of optimization settings to
add to your PostgreSQL config.
