-- M6-007C：登录会话登记与单会话强制下线。
-- 仅保存会话元数据（不保存任何令牌原文）；令牌通过 sid claim 关联本表。
CREATE TABLE IF NOT EXISTS user_sessions (
  id           text PRIMARY KEY,
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  username     text NOT NULL,
  ip           text NOT NULL DEFAULT '',
  user_agent   text NOT NULL DEFAULT '',
  created_at   timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL,
  revoked_at   timestamptz
);

CREATE INDEX IF NOT EXISTS user_sessions_user_idx
  ON user_sessions (user_id, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS user_sessions_expires_idx
  ON user_sessions (expires_at);

COMMENT ON TABLE user_sessions IS '登录会话（M6-007C）：/auth/sessions 展示与单会话强制下线';
COMMENT ON COLUMN user_sessions.id IS '会话 ID，写入 JWT sid claim';
COMMENT ON COLUMN user_sessions.revoked_at IS '非空=已强制下线/登出，令牌经 auth_token_blacklist(sess:<id>) 立即失效';
