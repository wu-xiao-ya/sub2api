-- Supporting indexes for the read-only administrator finance ledger.
-- Existing rows remain untouched; the ledger is assembled from source tables.

CREATE INDEX IF NOT EXISTS idx_payment_orders_balance_completed_at
    ON payment_orders (completed_at DESC)
    WHERE order_type = 'balance'
      AND status IN ('COMPLETED', 'PARTIALLY_REFUNDED', 'REFUNDED')
      AND completed_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_payment_audit_logs_refund_success_created_at
    ON payment_audit_logs (created_at DESC)
    WHERE action = 'REFUND_SUCCESS';

CREATE INDEX IF NOT EXISTS idx_redeem_codes_balance_used_at
    ON redeem_codes (used_at DESC)
    WHERE status = 'used'
      AND used_at IS NOT NULL
      AND type IN ('balance', 'admin_balance')
      AND value <> 0;

CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_transfer_created_at
    ON user_affiliate_ledger (created_at DESC)
    WHERE action = 'transfer' AND amount <> 0;
