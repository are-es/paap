package db

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

func Init(dataDir string) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	dbPath := filepath.Join(dataDir, "paap.db")
	conn, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return err
	}
	DB = conn
	return migrate()
}

func migrate() error {
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			icon TEXT DEFAULT '',
			is_active INTEGER DEFAULT 1,
			round_robin INTEGER DEFAULT 0,
			provider_type TEXT DEFAULT 'custom',
			builtin_id TEXT,
			round_robin_enabled INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
			name TEXT DEFAULT '',
			key_encrypted TEXT NOT NULL,
			account_id TEXT DEFAULT '',
			is_active INTEGER DEFAULT 1,
			fail_count INTEGER DEFAULT 0,
			last_error TEXT DEFAULT '',
			last_tested_at INTEGER DEFAULT 0,
			last_used DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS models (
			id TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
			model_id TEXT NOT NULL,
			is_free INTEGER DEFAULT 0,
			is_selected INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			icon TEXT DEFAULT '',
			round_robin INTEGER DEFAULT 1,
			inject_prompt TEXT DEFAULT '',
			inject_position TEXT DEFAULT 'prepend',
			race_mode TEXT DEFAULT 'round_robin',
			selected_keys TEXT DEFAULT '[]',
			selected_models TEXT DEFAULT '[]',
			race_count INTEGER DEFAULT 3,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS group_models (
			id TEXT PRIMARY KEY,
			group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			provider_id TEXT NOT NULL,
			model_id TEXT NOT NULL,
			position INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			provider_id TEXT,
			provider_name TEXT,
			model_id TEXT,
			key_id TEXT,
			key_name TEXT,
			group_name TEXT,
			framework TEXT,
			status_code INTEGER,
			race_status TEXT DEFAULT '',
			race_id TEXT DEFAULT '',
			tokens_in INTEGER DEFAULT 0,
			tokens_out INTEGER DEFAULT 0,
			latency_ms INTEGER DEFAULT 0,
			cost_usd REAL DEFAULT 0.0,
			compression_ratio REAL DEFAULT 0.0,
			skills_used TEXT DEFAULT '[]',
			error TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS gateway_keys (
			id TEXT PRIMARY KEY,
			key TEXT UNIQUE NOT NULL,
			name TEXT DEFAULT '',
			is_active INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS proxy_pools (
			id TEXT PRIMARY KEY,
			name TEXT DEFAULT '',
			address TEXT NOT NULL,
			port INTEGER NOT NULL,
			proxy_type TEXT DEFAULT 'socks5',
			username TEXT DEFAULT '',
			password TEXT DEFAULT '',
			is_active INTEGER DEFAULT 1,
			test_status TEXT DEFAULT 'unknown',
			test_ip TEXT DEFAULT '',
			test_region TEXT DEFAULT '',
			provider_id TEXT DEFAULT '',
			last_latency_ms INTEGER DEFAULT 0,
			success_count INTEGER DEFAULT 0,
			fail_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS ix_logs_ts ON logs(timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS ix_api_keys_provider ON api_keys(provider_id)`,
		`CREATE INDEX IF NOT EXISTS ix_models_provider ON models(provider_id)`,
		// Clean duplicates before creating unique index
		`DELETE FROM models WHERE rowid NOT IN (SELECT MIN(rowid) FROM models GROUP BY provider_id, model_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_models_provider_model ON models(provider_id, model_id)`,
		`CREATE INDEX IF NOT EXISTS ix_group_models_group ON group_models(group_id)`,
		`CREATE TABLE IF NOT EXISTS proxy_groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			is_active INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS proxy_group_members (
			id TEXT PRIMARY KEY,
			group_id TEXT NOT NULL REFERENCES proxy_groups(id) ON DELETE CASCADE,
			proxy_id TEXT NOT NULL REFERENCES proxy_pools(id) ON DELETE CASCADE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS ix_proxy_group_members_group ON proxy_group_members(group_id)`,
		`CREATE TABLE IF NOT EXISTS usage_stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			date TEXT NOT NULL,
			provider_id TEXT NOT NULL,
			provider_name TEXT NOT NULL,
			model_id TEXT NOT NULL,
			request_count INTEGER DEFAULT 0,
			success_count INTEGER DEFAULT 0,
			error_count INTEGER DEFAULT 0,
			tokens_in INTEGER DEFAULT 0,
			tokens_out INTEGER DEFAULT 0,
			total_cost_usd REAL DEFAULT 0.0,
			avg_latency_ms INTEGER DEFAULT 0,
			UNIQUE(date, provider_id, model_id)
		)`,
		`CREATE INDEX IF NOT EXISTS ix_usage_stats_date ON usage_stats(date DESC)`,
		`CREATE TABLE IF NOT EXISTS system_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// T02: New tables
		`CREATE TABLE IF NOT EXISTS provider_connections (
			id TEXT PRIMARY KEY,
			provider_id TEXT REFERENCES providers(id) ON DELETE CASCADE,
			auth_type TEXT DEFAULT 'apikey',
			name TEXT DEFAULT '',
			email TEXT DEFAULT '',
			api_key TEXT DEFAULT '',
			access_token TEXT DEFAULT '',
			refresh_token TEXT DEFAULT '',
			expires_at INTEGER DEFAULT 0,
			test_status TEXT DEFAULT 'untested',
			fail_count INTEGER DEFAULT 0,
			is_active INTEGER DEFAULT 1,
			created_at INTEGER DEFAULT 0,
			updated_at INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS compression_skills (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			intensity TEXT DEFAULT 'lite',
			categories TEXT DEFAULT '[]',
			custom_rules TEXT DEFAULT '[]',
			position INTEGER DEFAULT 0,
			is_active INTEGER DEFAULT 1,
			is_builtin INTEGER DEFAULT 0,
			created_at INTEGER DEFAULT 0,
			updated_at INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS cost_summary (
			id TEXT PRIMARY KEY,
			date TEXT NOT NULL,
			provider_id TEXT DEFAULT '',
			provider_name TEXT DEFAULT '',
			model_id TEXT DEFAULT '',
			req_count INTEGER DEFAULT 0,
			total_cost_usd REAL DEFAULT 0.0,
			total_tokens_in INTEGER DEFAULT 0,
			total_tokens_out INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS compression_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			model_id TEXT DEFAULT '',
			intensity TEXT DEFAULT '',
			messages_affected INTEGER DEFAULT 0,
			rules_applied INTEGER DEFAULT 0,
			orig_bytes INTEGER DEFAULT 0,
			new_bytes INTEGER DEFAULT 0,
			saved_percent REAL DEFAULT 0.0,
			latency_ms INTEGER DEFAULT 0,
			skills_used TEXT DEFAULT '[]'
		)`,
		`CREATE INDEX IF NOT EXISTS ix_compression_logs_ts ON compression_logs(timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS ix_provider_connections_provider ON provider_connections(provider_id)`,
		`CREATE INDEX IF NOT EXISTS ix_cost_summary_date ON cost_summary(date DESC)`,
	}
	for _, s := range schemas {
		if _, err := DB.Exec(s); err != nil {
			return err
		}
	}

	// === Migrations for existing tables (safe ALTER TABLE) ===
	DB.Exec("ALTER TABLE providers ADD COLUMN proxy_group_id TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE providers ADD COLUMN proxy_id TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE providers ADD COLUMN proxy_enabled INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE proxy_pools ADD COLUMN last_latency_ms INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE proxy_pools ADD COLUMN success_count INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE proxy_pools ADD COLUMN fail_count INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE api_keys ADD COLUMN account_id TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE api_keys ADD COLUMN key_type TEXT DEFAULT 'apikey'")
	DB.Exec("ALTER TABLE api_keys ADD COLUMN oauth_refresh_token TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE api_keys ADD COLUMN oauth_expires_at TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE groups ADD COLUMN parallel INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE logs ADD COLUMN race_status TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE logs ADD COLUMN race_id TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE logs ADD COLUMN proxy_used TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE providers ADD COLUMN auth_type TEXT DEFAULT 'apikey'")
	DB.Exec("ALTER TABLE providers ADD COLUMN oauth_access_token TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE providers ADD COLUMN oauth_refresh_token TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE providers ADD COLUMN oauth_expires_at TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE providers ADD COLUMN oauth_data TEXT DEFAULT ''")
	// T02: New columns for existing tables
	DB.Exec("ALTER TABLE providers ADD COLUMN provider_type TEXT DEFAULT 'custom'")
	DB.Exec("ALTER TABLE providers ADD COLUMN builtin_id TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE providers ADD COLUMN round_robin_enabled INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE api_keys ADD COLUMN fail_count INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE api_keys ADD COLUMN last_error TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE api_keys ADD COLUMN last_tested_at INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE groups ADD COLUMN race_mode TEXT DEFAULT 'round_robin'")
	DB.Exec("ALTER TABLE groups ADD COLUMN selected_keys TEXT DEFAULT '[]'")
	DB.Exec("ALTER TABLE groups ADD COLUMN selected_models TEXT DEFAULT '[]'")
	DB.Exec("ALTER TABLE groups ADD COLUMN race_count INTEGER DEFAULT 3")
	DB.Exec("ALTER TABLE groups ADD COLUMN max_keys INTEGER DEFAULT 10")
	DB.Exec("ALTER TABLE providers ADD COLUMN supports_anthropic INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE logs ADD COLUMN compression_ratio REAL DEFAULT 0.0")
	DB.Exec("ALTER TABLE logs ADD COLUMN skills_used TEXT DEFAULT '[]'")

	// === System settings defaults ===
	DB.Exec(`INSERT OR IGNORE INTO system_settings (key, value) VALUES ('race_apikeys', '10')`)
	DB.Exec(`INSERT OR IGNORE INTO system_settings (key, value) VALUES ('stealth_mode', '1')`)
	DB.Exec(`INSERT OR IGNORE INTO system_settings (key, value) VALUES ('compression_enabled', 'false')`)
	DB.Exec(`INSERT OR IGNORE INTO system_settings (key, value) VALUES ('compression_intensity', 'lite')`)
	DB.Exec(`INSERT OR IGNORE INTO system_settings (key, value) VALUES ('prompt_injection_enabled', 'false')`)
	DB.Exec(`INSERT OR IGNORE INTO system_settings (key, value) VALUES ('prompt_injection_text', '')`)
	DB.Exec(`INSERT OR IGNORE INTO system_settings (key, value) VALUES ('prompt_injection_position', 'prepend')`)
	DB.Exec(`INSERT OR IGNORE INTO system_settings (key, value) VALUES ('headroom_enabled', 'false')`)
	DB.Exec(`INSERT OR IGNORE INTO system_settings (key, value) VALUES ('headroom_url', 'http://127.0.0.1:8787')`)
	DB.Exec(`INSERT OR IGNORE INTO system_settings (key, value) VALUES ('headroom_timeout_ms', '15000')`)

	// === Seed claude-* prefixed groups for Claude Code (existing feature) ===
	// Only seed if no claude-* groups exist at all (first-time setup)
	// This prevents re-creating deleted claude-* groups on restart
	var claudeCount int
	DB.QueryRow("SELECT COUNT(*) FROM groups WHERE name LIKE 'claude-%'").Scan(&claudeCount)
	if claudeCount == 0 {
		// First time — seed claude-* groups from existing non-claude groups
		DB.Exec(`INSERT OR IGNORE INTO groups (id, name, round_robin) SELECT 'claude-' || id, 'claude-' || name, round_robin FROM groups WHERE name NOT LIKE 'claude-%'`)
		DB.Exec(`INSERT OR IGNORE INTO group_models (id, group_id, provider_id, model_id, position)
			SELECT 'claude-' || gm.id, 'claude-' || gm.group_id, gm.provider_id, gm.model_id, gm.position
			FROM group_models gm
			JOIN groups g ON gm.group_id = g.id
			WHERE g.name NOT LIKE 'claude-%'
			AND NOT EXISTS (SELECT 1 FROM group_models WHERE id = 'claude-' || gm.id)`)
	}

	// === T02: Delete old seed providers ===
	// TokenGO: explicitly removed per task spec
	DB.Exec(`DELETE FROM providers WHERE LOWER(name) = 'tokengo' OR base_url LIKE '%tokengo.com%'`)
	// Old Meta seed (replaced by builtin-meta)
	DB.Exec(`DELETE FROM providers WHERE id = 'meta-default'`)
	// Old grok-cli seed (replaced by builtin-grok-cli)
	DB.Exec(`DELETE FROM providers WHERE id = 'grok-cli'`)
	// Old Xiaomi MiMo seed (if exists)
	DB.Exec(`DELETE FROM providers WHERE id = 'mimo-default' OR (LOWER(name) = 'xiaomi mimo' AND (provider_type = '' OR provider_type = 'custom' OR provider_type IS NULL))`)

	// === T02: Seed 9 built-in providers ===
	now := `strftime('%s', 'now')`
	DB.Exec(`INSERT OR IGNORE INTO providers (id, name, base_url, icon, is_active, round_robin, provider_type, auth_type, builtin_id, round_robin_enabled, supports_anthropic, created_at, updated_at)
		VALUES ('builtin-xiaomi', 'Xiaomi (MiMo)', 'https://api.xiaomimimo.com/v1', 'xiaomi.svg', 1, 1, 'builtin', 'apikey', 'xiaomi', 1, 1, `+now+`, `+now+`)`)
	DB.Exec(`INSERT OR IGNORE INTO providers (id, name, base_url, icon, is_active, round_robin, provider_type, auth_type, builtin_id, round_robin_enabled, supports_anthropic, created_at, updated_at)
		VALUES ('builtin-meta', 'Meta (Llama)', 'https://api.meta.ai/v1', 'meta.svg', 1, 1, 'builtin', 'apikey', 'meta', 1, 1, `+now+`, `+now+`)`)
	DB.Exec(`INSERT OR IGNORE INTO providers (id, name, base_url, icon, is_active, round_robin, provider_type, auth_type, builtin_id, round_robin_enabled, supports_anthropic, created_at, updated_at)
		VALUES ('builtin-google', 'Google AI Studio (Gemini)', 'https://generativelanguage.googleapis.com/v1beta', 'google.svg', 1, 1, 'builtin', 'apikey', 'google', 1, 0, `+now+`, `+now+`)`)
	DB.Exec(`INSERT OR IGNORE INTO providers (id, name, base_url, icon, is_active, round_robin, provider_type, auth_type, builtin_id, round_robin_enabled, supports_anthropic, created_at, updated_at)
		VALUES ('builtin-kimchi', 'Kimchi', 'https://llm.kimchi.dev/openai/v1', 'kimchi.svg', 1, 1, 'builtin', 'apikey', 'kimchi', 1, 0, `+now+`, `+now+`)`)
	DB.Exec(`INSERT OR IGNORE INTO providers (id, name, base_url, icon, is_active, round_robin, provider_type, auth_type, builtin_id, round_robin_enabled, supports_anthropic, created_at, updated_at)
		VALUES ('builtin-openrouter', 'OpenRouter', 'https://openrouter.ai/api/v1', 'openrouter.svg', 1, 1, 'builtin', 'apikey', 'openrouter', 1, 1, `+now+`, `+now+`)`)
	DB.Exec(`INSERT OR IGNORE INTO providers (id, name, base_url, icon, is_active, round_robin, provider_type, auth_type, builtin_id, round_robin_enabled, supports_anthropic, created_at, updated_at)
		VALUES ('builtin-grok-cli', 'Grok CLI', 'https://cli-chat-proxy.grok.com/v1', 'grok.ico', 1, 0, 'builtin', 'connection', 'grok-cli', 0, 0, `+now+`, `+now+`)`)
	DB.Exec(`INSERT OR IGNORE INTO providers (id, name, base_url, icon, is_active, round_robin, provider_type, auth_type, builtin_id, round_robin_enabled, supports_anthropic, created_at, updated_at)
		VALUES ('builtin-anigravity', 'Anigravity CLI', 'https://api.anigravity.ai/v1', 'anigravity.svg', 1, 1, 'builtin', 'connection', 'anigravity', 1, 0, `+now+`, `+now+`)`)
	DB.Exec(`INSERT OR IGNORE INTO providers (id, name, base_url, icon, is_active, round_robin, provider_type, auth_type, builtin_id, round_robin_enabled, supports_anthropic, created_at, updated_at)
		VALUES ('builtin-ollamacloud', 'OllamaCloud', 'https://api.ollamacloud.ai/v1', 'ollama.svg', 1, 1, 'builtin', 'apikey', 'ollamacloud', 1, 1, `+now+`, `+now+`)`)
	DB.Exec(`INSERT OR IGNORE INTO providers (id, name, base_url, icon, is_active, round_robin, provider_type, auth_type, builtin_id, round_robin_enabled, supports_anthropic, created_at, updated_at)
		VALUES ('builtin-runapi', 'RunAPI', 'https://runapi.co/v1', 'runapi.svg', 1, 1, 'builtin', 'apikey', 'runapi', 1, 1, `+now+`, `+now+`)`)

	// === Builtin compression skills REMOVED — use folder-based skills only ===
	// Skills are loaded from ~/.paap/skills/ folder (JSON/MD files)
	// No more builtin skills in DB

	return nil
}

func Close() {
	if DB != nil {
		DB.Close()
	}
}
