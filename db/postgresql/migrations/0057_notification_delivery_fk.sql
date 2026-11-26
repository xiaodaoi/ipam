-- 0057_notification_delivery_fk.sql — 投递记录随报警级联清理（M6-068）
--
-- notification_delivery 只有索引、没有外键：alert 按保留期清理后会留下孤儿行，
-- 且恢复/删除通道后历史记录无法判定归属。先清理存量孤儿，再加级联外键。

DELETE FROM notification_delivery d
 WHERE NOT EXISTS (SELECT 1 FROM alert a WHERE a.id = d.alert_id);

ALTER TABLE notification_delivery
  DROP CONSTRAINT IF EXISTS notification_delivery_alert_id_fkey;
ALTER TABLE notification_delivery
  ADD CONSTRAINT notification_delivery_alert_id_fkey
  FOREIGN KEY (alert_id) REFERENCES alert(id) ON DELETE CASCADE;
