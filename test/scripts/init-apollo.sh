#!/bin/bash
set -e

# Paths to local SQL files
CONFIG_DB_SQL="/docker-entrypoint-initdb.d/sql/apolloconfigdb.sql"
PORTAL_DB_SQL="/docker-entrypoint-initdb.d/sql/apolloportaldb.sql"
INIT_CONFIG_SQL="/docker-entrypoint-initdb.d/sql/init-config.sql"

# Check if SQL files exist
if [ ! -f "$CONFIG_DB_SQL" ]; then
    echo "Error: $CONFIG_DB_SQL not found."
    exit 1
fi

if [ ! -f "$PORTAL_DB_SQL" ]; then
    echo "Error: $PORTAL_DB_SQL not found."
    exit 1
fi

# Wait for MySQL to be ready
until mysqladmin ping -h localhost -u root -p"$MYSQL_ROOT_PASSWORD" --silent; do
    echo "Waiting for MySQL to be ready..."
    sleep 2
done

# Execute SQL files
mysql -u root -p"$MYSQL_ROOT_PASSWORD" -e "CREATE DATABASE IF NOT EXISTS ApolloConfigDB;"
mysql -u root -p"$MYSQL_ROOT_PASSWORD" -e "CREATE DATABASE IF NOT EXISTS ApolloPortalDB;"
mysql -u root -p"$MYSQL_ROOT_PASSWORD" ApolloConfigDB < "$CONFIG_DB_SQL"
mysql -u root -p"$MYSQL_ROOT_PASSWORD" ApolloPortalDB < "$PORTAL_DB_SQL"

# Execute init-config.sql for xlayer-go-app configuration
if [ -f "$INIT_CONFIG_SQL" ]; then
    mysql -u root -p"$MYSQL_ROOT_PASSWORD" < "$INIT_CONFIG_SQL"
    echo "init-config.sql executed successfully."
else
    echo "Warning: $INIT_CONFIG_SQL not found."
fi

echo "Apollo databases initialized successfully."