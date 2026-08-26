-- Store the user's single opt-in for charging wallet balance after shared
-- subscription quota or subscription concurrency is exhausted.
CREATE TABLE IF NOT EXISTS user_subscription_preferences (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    balance_topup_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
