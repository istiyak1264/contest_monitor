-- This file runs automatically when the MySQL Docker container first starts.
-- It ensures the database and base tables exist.

CREATE DATABASE IF NOT EXISTS auth_db;
USE auth_db;

-- Users table (GORM will also auto-migrate this, but having it here is safe)
CREATE TABLE IF NOT EXISTS users (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name       VARCHAR(255),
    email      VARCHAR(255) UNIQUE,
    password   VARCHAR(255)
);

-- Contests table
CREATE TABLE IF NOT EXISTS contests (
    id                  BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name                VARCHAR(255),
    start_time          DATETIME,
    end_time            DATETIME,
    table_name          VARCHAR(255),
    traffic_logs_table  VARCHAR(255),
    ai_hits_table       VARCHAR(255)
);
