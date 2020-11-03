# Running dcrpool with PostgreSQL

Tested with PostgreSQL 13.0.

**Note:** When running in Postgres mode, backups will not be created
automatically by dcrpool.

1. Connect to your instance of PostgreSQL using `psql` to create a new database
    and a new user for dcrpool.
    Be sure to substitute the example password `12345` with something more secure.

    ```no-highlight
    postgres=# CREATE DATABASE dcrpooldb;
    CREATE DATABASE
    postgres=# CREATE USER dcrpooluser WITH ENCRYPTED PASSWORD '12345';
    CREATE ROLE
    postgres=# GRANT ALL PRIVILEGES ON DATABASE dcrpooldb to dcrpooluser;
    GRANT
    ```

1. **Developers only** - if you are modifying code and wish to run the dcrpool
   test suite, you will need to create an additional database.

    ```no-highlight
    postgres=# CREATE DATABASE dcrpooltestdb;
    CREATE DATABASE
    postgres=# GRANT ALL PRIVILEGES ON DATABASE dcrpooltestdb to dcrpooluser;
    GRANT
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
