CREATE TABLE IF NOT EXISTS regions (
    id                    TEXT PRIMARY KEY,
    name                  TEXT NOT NULL,
    lat                   REAL NOT NULL,
    lon                   REAL NOT NULL,
    friction_tier         TEXT NOT NULL,
    near_threshold_in     REAL NOT NULL,
    extended_threshold_in REAL NOT NULL,
    country               TEXT NOT NULL,
    nearest_airport       TEXT NOT NULL DEFAULT '',
    drive_time_hours      REAL NOT NULL DEFAULT 0,
    drive_notes           TEXT NOT NULL DEFAULT '',
    lodging_notes         TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS resorts (
    id                  TEXT PRIMARY KEY,
    region_id           TEXT NOT NULL REFERENCES regions(id),
    name                TEXT NOT NULL,
    lat                 REAL NOT NULL,
    lon                 REAL NOT NULL,
    summit_elevation_ft INTEGER NOT NULL DEFAULT 0,
    base_elevation_ft   INTEGER NOT NULL DEFAULT 0,
    vertical_drop_ft    INTEGER NOT NULL DEFAULT 0,
    skiable_acres       INTEGER NOT NULL DEFAULT 0,
    lift_count          INTEGER NOT NULL,
    pass_affiliations   TEXT NOT NULL DEFAULT '[]',
    metadata            TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS storms (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    region_id         TEXT NOT NULL,
    window_start      TEXT NOT NULL,
    window_end        TEXT NOT NULL,
    state             TEXT NOT NULL,
    current_tier      TEXT NOT NULL DEFAULT '',
    discord_thread_id TEXT NOT NULL DEFAULT '',
    detected_at       TEXT NOT NULL,
    last_evaluated_at TEXT NOT NULL DEFAULT '',
    last_posted_at    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS evaluations (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    storm_id            INTEGER NOT NULL REFERENCES storms(id),
    evaluated_at        TEXT NOT NULL,
    prompt_version      TEXT NOT NULL,
    tier                TEXT NOT NULL,
    recommendation      TEXT NOT NULL,
    day_by_day          TEXT NOT NULL DEFAULT '[]',
    key_factors         TEXT NOT NULL DEFAULT '{}',
    logistics_summary   TEXT NOT NULL DEFAULT '{}',
    strategy            TEXT NOT NULL DEFAULT '',
    snow_quality        TEXT NOT NULL DEFAULT '',
    crowd_estimate      TEXT NOT NULL DEFAULT '',
    closure_risk        TEXT NOT NULL DEFAULT '',
    weather_snapshot    TEXT NOT NULL DEFAULT '[]',
    raw_llm_response    TEXT NOT NULL DEFAULT '',
    structured_response TEXT NOT NULL DEFAULT '{}',
    grounding_sources   TEXT NOT NULL DEFAULT '[]',
    change_class        TEXT NOT NULL DEFAULT '',
    delivered           INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS user_profiles (
    id                   INTEGER PRIMARY KEY,
    home_base            TEXT NOT NULL DEFAULT '',
    home_lat             REAL NOT NULL DEFAULT 0,
    home_lon             REAL NOT NULL DEFAULT 0,
    passes_held          TEXT NOT NULL DEFAULT '[]',
    skill_level          TEXT NOT NULL DEFAULT '',
    preferences          TEXT NOT NULL DEFAULT '',
    remote_work_capable  INTEGER NOT NULL DEFAULT 0,
    typical_pto_days     INTEGER NOT NULL DEFAULT 0,
    blackout_dates       TEXT NOT NULL DEFAULT '[]',
    min_tier_for_ping    TEXT NOT NULL DEFAULT '',
    quiet_hours_start    TEXT NOT NULL DEFAULT '',
    quiet_hours_end      TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS prompt_templates (
    id         TEXT NOT NULL,
    version    TEXT NOT NULL,
    template   TEXT NOT NULL,
    created_at TEXT NOT NULL,
    is_active  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (id, version)
);

CREATE TABLE IF NOT EXISTS eval_costs (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    storm_id           INTEGER NOT NULL REFERENCES storms(id),
    region_id          TEXT NOT NULL,
    evaluated_at       TEXT NOT NULL,
    estimated_cost_usd REAL NOT NULL,
    success            INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_storms_region_state ON storms(region_id, state);
CREATE INDEX IF NOT EXISTS idx_evaluations_storm ON evaluations(storm_id);
CREATE INDEX IF NOT EXISTS idx_eval_costs_month ON eval_costs(evaluated_at);
