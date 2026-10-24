-- 充值赠金/折扣：记录订单按实付金额命中阶梯后的赠送额度（折扣模式下为折算差额）。
-- 空值语义：0 表示无赠金；历史订单查询保持原行为。
ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS bonus_amount DECIMAL(20,2) NOT NULL DEFAULT 0;
