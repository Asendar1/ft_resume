-- Integrity lives here, not in Go: length bounds, status enum, one row per company+role.
CREATE TABLE IF NOT EXISTS applications (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    company    TEXT NOT NULL COLLATE NOCASE CHECK (length(trim(company)) BETWEEN 1 AND 120),
    role       TEXT NOT NULL COLLATE NOCASE CHECK (length(trim(role)) BETWEEN 1 AND 120),
    status     TEXT NOT NULL DEFAULT 'applied'
               CHECK (status IN ('applied', 'screen', 'interview', 'offer', 'rejected')),
    notes      TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 2000),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (company, role)
);
