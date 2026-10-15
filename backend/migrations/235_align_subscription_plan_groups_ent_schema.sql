-- Align the plan-to-group relation table with the generated Ent entity.
--
-- Migration 231 intentionally used a composite primary key and did not add
-- an implicit Ent id column. The generated SubscriptionPlanGroup entity does
-- select id, so queries through that entity failed against databases created
-- by migration 231. Keep the composite-key semantics and add a unique,
-- auto-generated id for Ent compatibility.

CREATE SEQUENCE IF NOT EXISTS subscription_plan_groups_id_seq;

ALTER TABLE subscription_plan_groups
    ADD COLUMN IF NOT EXISTS id BIGINT;

ALTER SEQUENCE subscription_plan_groups_id_seq
    OWNED BY subscription_plan_groups.id;

ALTER TABLE subscription_plan_groups
    ALTER COLUMN id SET DEFAULT nextval('subscription_plan_groups_id_seq');

UPDATE subscription_plan_groups
SET id = nextval('subscription_plan_groups_id_seq')
WHERE id IS NULL;

SELECT setval(
    'subscription_plan_groups_id_seq',
    COALESCE((SELECT MAX(id) FROM subscription_plan_groups), 0) + 1,
    false
);

ALTER TABLE subscription_plan_groups
    ALTER COLUMN id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_plan_groups_id
    ON subscription_plan_groups(id);
