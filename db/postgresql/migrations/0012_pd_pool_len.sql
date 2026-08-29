-- 0012: PD 前缀委派池（M2-018；kind=pd 时两列必填语义由服务端保证）
ALTER TABLE address_pool ADD COLUMN IF NOT EXISTS prefix_len int;
ALTER TABLE address_pool ADD COLUMN IF NOT EXISTS delegated_len int;
