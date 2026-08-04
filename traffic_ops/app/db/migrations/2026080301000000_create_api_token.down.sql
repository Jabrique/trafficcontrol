-- Rollback: drop api_token table and all its indexes (CASCADE drops indexes automatically)
-- Migration: 2026080301000000_create_api_token.down.sql

DROP TABLE IF EXISTS api_token;
