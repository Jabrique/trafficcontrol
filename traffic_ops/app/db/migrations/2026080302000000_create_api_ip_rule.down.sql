-- Rollback: drop api_ip_rule table and all its indexes (CASCADE drops indexes automatically)
-- Seed rule is removed automatically as part of DROP TABLE.
-- Migration: 2026080302000000_create_api_ip_rule.down.sql

DROP TABLE IF EXISTS api_ip_rule;
