"""
数据库重复用户清理脚本 v2
将 SQL 文件上传到服务器后执行，避免 shell 转义问题。
"""

import os, sys, tempfile, io
import paramiko

SSH_HOST = os.environ.get("SSH_HOST", "192.168.0.135")
SSH_USER = os.environ.get("SSH_USER", "root")
SSH_PASS = os.environ.get("SSH_PASS", "")

if not SSH_PASS:
    print("请设置 SSH_PASS 环境变量")
    sys.exit(1)

ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect(SSH_HOST, username=SSH_USER, password=SSH_PASS, timeout=30)
sftp = ssh.open_sftp()
print(f"✓ 已连接 {SSH_HOST}")


def run(cmd, timeout=60, show=True):
    if show:
        print(f"  $ {cmd[:100]}")
    _, out, err = ssh.exec_command(cmd, timeout=timeout)
    o = out.read().decode("utf-8", errors="replace")
    e = err.read().decode("utf-8", errors="replace")
    if o.strip():
        print(o.rstrip())
    if e.strip():
        print("ERR:", e.rstrip(), file=sys.stderr)
    return o


def run_sql(sql: str, label: str = ""):
    """将 SQL 上传为临时文件，在容器内执行。"""
    if label:
        print(f"\n── {label}")
    remote_path = "/tmp/_dedup_tmp.sql"
    sftp.putfo(io.BytesIO(sql.encode("utf-8")), remote_path)
    run(
        f"docker cp {remote_path} token-monitor-db-1:/tmp/_dedup.sql && "
        f"docker exec token-monitor-db-1 psql -U monitor -d token_monitor -f /tmp/_dedup.sql",
        timeout=120,
    )


# ─────────────────────────────────────────────────────────────
# 合并映射: (zombie_id, canonical_id, new_email_for_canonical|None)
# ─────────────────────────────────────────────────────────────
MERGE_MAP = [
    # 陈智 → 37 (10023, chenzhi@otw.cn)
    (57,  37,  None),
    (58,  37,  None),
    (142, 37,  None),
    # 陈卫发 → 67
    (68,  67,  None),
    # 邓卫卫 → 104
    (69,  104, None),
    (72,  104, None),
    (105, 104, None),
    (137, 104, None),
    # 关振基 → 120
    (131, 120, None),
    # 郭鹏辉 → 47
    (56,  47,  None),
    # 韩佩燕 → 112
    (113, 112, None),
    (134, 112, None),
    # 姜玮鹏 → 89
    (110, 89,  None),
    (123, 89,  None),
    (140, 89,  None),
    # 焦飞 → 92
    (93,  92,  None),
    # 李鹏 → 59
    (121, 59,  None),
    (138, 59,  None),
    # 李杰 → 79
    (80,  79,  None),
    (141, 79,  None),
    # 李子腾 → 75
    (76,  75,  None),
    # 李欣荷 → 43
    (124, 43,  None),
    # 李志颖 → 102
    (103, 102, None),
    # 李越 → 127
    (128, 127, None),
    # 马杰 → 106
    (107, 106, None),
    # mmy → 108
    (111, 108, None),
    # 宁峰 → 42
    (129, 42,  None),
    (136, 42,  None),
    # 佘林 → 100
    (101, 100, None),
    # 宋涛杰 → 40
    (125, 40,  None),
    # 王朝阳 → 84
    (85,  84,  None),
    # 王栋 → 132
    (133, 132, None),
    # 王瑞博 → 60
    (70,  60,  None),
    # 王特 → 71
    (73,  71,  None),
    (122, 71,  None),
    (139, 71,  None),
    # 王一博: 109 有真实邮箱 wangyibo@otw.cn，合并进 45 并更新 45 的 email
    (109, 45,  "wangyibo@otw.cn"),
    (116, 45,  None),
    (135, 45,  None),
    # 汪尊 → 118
    (119, 118, None),
    # 熊德超 → 63
    (64,  63,  None),
    # 许江博 → 44
    (98,  44,  None),
    # 闫栋/yandong → 86
    (97,  86,  None),
    (126, 86,  None),
    # 杨光 → 61
    (62,  61,  None),
    # 杨欢 → 114
    (115, 114, None),
    (117, 114, None),
    # 赵自勇 → 95
    (96,  95,  None),
    # 赵宇星 → 81
    (82,  81,  None),
    # 郑甜甜 → 55
    (130, 55,  None),
    # 周亮 → 91
    (94,  91,  None),
]

zombie_ids = [z for z, _, _ in MERGE_MAP]
canonical_ids = {c for _, c, _ in MERGE_MAP}
print(f"共 {len(zombie_ids)} 个僵尸用户 → {len(canonical_ids)} 个真实用户")

# ─────────────────────────────────────────────────────────────
# 步骤 0: 备份
# ─────────────────────────────────────────────────────────────
run_sql("""
DROP TABLE IF EXISTS users_backup_before_dedup;
CREATE TABLE users_backup_before_dedup AS SELECT * FROM users;
SELECT COUNT(*) AS backup_rows FROM users_backup_before_dedup;
""", "备份用户表")

# ─────────────────────────────────────────────────────────────
# 步骤 1: 迁移 token_usage_logs
# ─────────────────────────────────────────────────────────────
cases = "\n".join(f"    WHEN {z} THEN {c}" for z, c, _ in MERGE_MAP)
z_list = ",".join(str(z) for z in zombie_ids)
run_sql(f"""
UPDATE token_usage_logs
SET user_id = CASE user_id
{cases}
  ELSE user_id
END
WHERE user_id IN ({z_list});
SELECT 'token_usage_logs migrated: ' || COUNT(*) FROM token_usage_logs WHERE user_id IN ({",".join(str(c) for c in canonical_ids)});
""", "迁移 token_usage_logs")

# ─────────────────────────────────────────────────────────────
# 步骤 2: 合并 daily_usage_summary（逐个处理，避免唯一约束冲突）
# ─────────────────────────────────────────────────────────────
# 构建一个大 SQL 处理所有合并
merge_blocks = []
for zombie_id, canonical_id, _ in MERGE_MAP:
    merge_blocks.append(f"""
-- zombie {zombie_id} → canonical {canonical_id}
UPDATE daily_usage_summary AS c
SET
  total_requests = c.total_requests + z.total_requests,
  input_tokens   = c.input_tokens   + z.input_tokens,
  output_tokens  = c.output_tokens  + z.output_tokens,
  total_tokens   = c.total_tokens   + z.total_tokens,
  cost_usd       = c.cost_usd       + z.cost_usd,
  cost_cny       = c.cost_cny       + z.cost_cny
FROM daily_usage_summary z
WHERE z.user_id = {zombie_id}
  AND c.user_id = {canonical_id}
  AND c.date      = z.date
  AND c.proj_key  = z.proj_key
  AND c.model_name = z.model_name
  AND c.provider  = z.provider
  AND c.dept_key  = z.dept_key;

UPDATE daily_usage_summary
SET user_id = {canonical_id}
WHERE user_id = {zombie_id}
  AND NOT EXISTS (
    SELECT 1 FROM daily_usage_summary d2
    WHERE d2.user_id = {canonical_id}
      AND d2.date      = daily_usage_summary.date
      AND d2.proj_key  = daily_usage_summary.proj_key
      AND d2.model_name = daily_usage_summary.model_name
      AND d2.provider  = daily_usage_summary.provider
      AND d2.dept_key  = daily_usage_summary.dept_key
  );

DELETE FROM daily_usage_summary WHERE user_id = {zombie_id};
""")

run_sql("\n".join(merge_blocks), "合并 daily_usage_summary")
run_sql("SELECT COUNT(*) AS remaining_zombie_summary FROM daily_usage_summary WHERE user_id IN (" + z_list + ");")

# ─────────────────────────────────────────────────────────────
# 步骤 3: 更新 canonical 用户邮箱
# ─────────────────────────────────────────────────────────────
email_updates = [(c, email) for _, c, email in MERGE_MAP if email]
if email_updates:
    email_sql = "\n".join(
        f"UPDATE users SET email = '{email}' WHERE id = {cid};"
        for cid, email in email_updates
    )
    run_sql(email_sql, "更新 canonical 用户邮箱")

# ─────────────────────────────────────────────────────────────
# 步骤 4: 删除僵尸用户
# ─────────────────────────────────────────────────────────────
run_sql(f"""
DELETE FROM users WHERE id IN ({z_list});
SELECT COUNT(*) AS remaining_users FROM users;
""", "删除僵尸用户")

# ─────────────────────────────────────────────────────────────
# 步骤 5: 验证
# ─────────────────────────────────────────────────────────────
run_sql("""
SELECT
  u.id,
  u.employee_id,
  u.name,
  u.email,
  u.password_hash IS NOT NULL AS has_pwd,
  u.auth_token IS NOT NULL AS has_token,
  COUNT(DISTINCT l.id) AS log_count,
  COUNT(DISTINCT s.id) AS summary_count
FROM users u
LEFT JOIN token_usage_logs l ON l.user_id = u.id
LEFT JOIN daily_usage_summary s ON s.user_id = u.id
GROUP BY u.id, u.employee_id, u.name, u.email, u.password_hash, u.auth_token
ORDER BY u.employee_id;
""", "最终用户列表")

run_sql("""
SELECT
  COUNT(*) AS total_users,
  SUM(CASE WHEN password_hash IS NOT NULL THEN 1 ELSE 0 END) AS with_auth,
  SUM(CASE WHEN password_hash IS NULL THEN 1 ELSE 0 END) AS no_auth
FROM users;

SELECT COUNT(*) AS orphan_logs
FROM token_usage_logs
WHERE user_id IS NOT NULL
  AND user_id NOT IN (SELECT id FROM users);
""", "汇总统计 + 孤儿日志检查")

print("\n✅ 数据库去重完成")
sftp.close()
ssh.close()
