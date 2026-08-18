/**
 * Client holiday cache via official SQLite WASM (OPFS when available).
 * Cache/offline only — never stores secrets. See /ARCHITECTURE.md.
 */

const WASM_URL = new URL("../wasm/sqlite3.mjs", import.meta.url).href;
const DB_FILENAME = "adayisaholiday-cache.sqlite3";
const META_VERSION = "dataset_version";
const META_IMPORTED_AT = "imported_at";

/**
 * @typedef {{ date: string, month: number, day: number, name: string, category: string, country?: string|null }} Holiday
 */

/** @type {null | { mode: 'sqlite'|'json', backend: string, getByDate: Function, queryDays: Function, categories: Function, count: Function }} */
let api = null;

async function sha256Hex(text) {
  const buf = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return [...new Uint8Array(buf)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

async function openDb(sqlite3) {
  if (typeof sqlite3.installOpfsSAHPoolVfs === "function") {
    try {
      const pool = await sqlite3.installOpfsSAHPoolVfs({
        name: "adayisaholiday-opfs",
      });
      if (pool?.OpfsSAHPoolDb) {
        return {
          db: new pool.OpfsSAHPoolDb(DB_FILENAME),
          backend: "opfs-sahpool",
        };
      }
    } catch (e) {
      console.warn("OPFS SAH pool unavailable, trying OpfsDb/memory:", e);
    }
  }
  if (sqlite3?.oo1?.OpfsDb) {
    try {
      return { db: new sqlite3.oo1.OpfsDb(DB_FILENAME), backend: "opfs" };
    } catch (e) {
      console.warn("OPFS DB open failed, using memory:", e);
    }
  }
  return { db: new sqlite3.oo1.DB(":memory:", "c"), backend: "memory" };
}

function ensureSchema(db) {
  db.exec(`
    CREATE TABLE IF NOT EXISTS meta (
      key TEXT PRIMARY KEY NOT NULL,
      value TEXT NOT NULL
    );
    CREATE TABLE IF NOT EXISTS holidays (
      id INTEGER PRIMARY KEY,
      date TEXT NOT NULL,
      month INTEGER NOT NULL,
      day INTEGER NOT NULL,
      name TEXT NOT NULL,
      category TEXT NOT NULL,
      country TEXT
    );
    CREATE INDEX IF NOT EXISTS idx_holidays_date ON holidays(date);
    CREATE INDEX IF NOT EXISTS idx_holidays_month ON holidays(month);
    CREATE INDEX IF NOT EXISTS idx_holidays_category ON holidays(category);
  `);
}

function metaGet(db, key) {
  let value = null;
  db.exec({
    sql: "SELECT value FROM meta WHERE key = ?",
    bind: [key],
    rowMode: "array",
    callback: (row) => {
      value = row[0];
    },
  });
  return value;
}

function metaSet(db, key, value) {
  db.exec({
    sql: "INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
    bind: [key, value],
  });
}

function holidayCount(db) {
  let n = 0;
  db.exec({
    sql: "SELECT COUNT(*) FROM holidays",
    rowMode: "array",
    callback: (row) => {
      n = row[0];
    },
  });
  return n;
}

function importHolidays(db, holidays, version) {
  db.exec("BEGIN");
  try {
    db.exec("DELETE FROM holidays");
    const stmt = db.prepare(
      "INSERT INTO holidays(date, month, day, name, category, country) VALUES(?,?,?,?,?,?)"
    );
    try {
      for (const h of holidays) {
        stmt.bind([
          h.date,
          h.month,
          h.day,
          h.name,
          h.category,
          h.country ?? null,
        ]).stepReset();
      }
    } finally {
      stmt.finalize();
    }
    metaSet(db, META_VERSION, version);
    metaSet(db, META_IMPORTED_AT, new Date().toISOString());
    db.exec("COMMIT");
  } catch (e) {
    db.exec("ROLLBACK");
    throw e;
  }
}

function rowToHoliday(row) {
  return {
    date: row[0],
    month: row[1],
    day: row[2],
    name: row[3],
    category: row[4],
    country: row[5] || undefined,
  };
}

function makeSqliteApi(db, backend) {
  return {
    mode: "sqlite",
    backend,
    count() {
      return holidayCount(db);
    },
    categories() {
      const cats = [];
      db.exec({
        sql: "SELECT DISTINCT category FROM holidays ORDER BY category",
        rowMode: "array",
        callback: (row) => cats.push(row[0]),
      });
      return cats;
    },
    /** @param {string} mmdd */
    getByDate(mmdd) {
      const out = [];
      db.exec({
        sql: "SELECT date, month, day, name, category, country FROM holidays WHERE date = ? ORDER BY name",
        bind: [mmdd],
        rowMode: "array",
        callback: (row) => out.push(rowToHoliday(row)),
      });
      return out;
    },
    /**
     * @param {{ search?: string, month?: number|null, category?: string }} filters
     * month is 0-based calendar month, or null/undefined for all
     */
    queryDays(filters = {}) {
      const search = (filters.search || "").trim().toLowerCase();
      const month = filters.month;
      const category = filters.category || "";
      const like = `%${search}%`;
      const out = [];
      const sql = `
        SELECT date, month, day, name, category, country
        FROM holidays
        WHERE (? IS NULL OR month = ?)
          AND (? = '' OR category = ?)
          AND (? = '' OR LOWER(name) LIKE ? OR LOWER(COALESCE(country, '')) LIKE ?)
        ORDER BY month, day, name
      `;
      const monthBind = month === null || month === undefined || month === "" ? null : Number(month) + 1;
      db.exec({
        sql,
        bind: [monthBind, monthBind, category, category, search, like, like],
        rowMode: "array",
        callback: (row) => out.push(rowToHoliday(row)),
      });
      return out;
    },
  };
}

function makeJsonApi(holidays) {
  return {
    mode: "json",
    backend: "memory-array",
    count() {
      return holidays.length;
    },
    categories() {
      return [...new Set(holidays.map((h) => h.category))].sort();
    },
    getByDate(mmdd) {
      return holidays.filter((h) => h.date === mmdd);
    },
    queryDays(filters = {}) {
      const search = (filters.search || "").trim().toLowerCase();
      const month = filters.month;
      const category = filters.category || "";
      const monthNum =
        month === null || month === undefined || month === "" ? null : Number(month) + 1;
      return holidays.filter((h) => {
        if (monthNum !== null && h.month !== monthNum) return false;
        if (category && h.category !== category) return false;
        if (search) {
          const hay = `${h.name} ${h.country || ""}`.toLowerCase();
          if (!hay.includes(search)) return false;
        }
        return true;
      });
    },
  };
}

async function initSqliteFromJson(jsonText, holidays) {
  const { default: sqlite3InitModule } = await import(WASM_URL);
  const sqlite3 = await sqlite3InitModule({
    locateFile: (path) => new URL(`../wasm/${path}`, import.meta.url).href,
  });
  const { db, backend } = await openDb(sqlite3);
  ensureSchema(db);
  const version = await sha256Hex(jsonText);
  const existing = metaGet(db, META_VERSION);
  const n = holidayCount(db);
  if (existing !== version || n === 0) {
    importHolidays(db, holidays, version);
  }
  return makeSqliteApi(db, backend);
}

async function initSqliteOfflineOnly() {
  const { default: sqlite3InitModule } = await import(WASM_URL);
  const sqlite3 = await sqlite3InitModule({
    locateFile: (path) => new URL(`../wasm/${path}`, import.meta.url).href,
  });
  const { db, backend } = await openDb(sqlite3);
  if (backend === "memory") {
    db.close();
    throw new Error("No persistent OPFS cache for offline use");
  }
  ensureSchema(db);
  if (holidayCount(db) === 0) {
    db.close();
    throw new Error("Empty OPFS cache");
  }
  return makeSqliteApi(db, `${backend}-offline`);
}

/**
 * Initialize holiday data layer. Prefers SQLite WASM; falls back to in-memory JSON.
 * @returns {Promise<typeof api>}
 */
export async function initHolidaysDb() {
  if (api) return api;

  let jsonText = null;
  let holidays = null;
  let fetchErr = null;
  try {
    const res = await fetch("holidays.json", { cache: "no-cache" });
    if (!res.ok) throw new Error(`holidays.json HTTP ${res.status}`);
    jsonText = await res.text();
    holidays = JSON.parse(jsonText);
    if (!Array.isArray(holidays)) throw new Error("holidays.json must be an array");
  } catch (e) {
    fetchErr = e;
  }

  if (holidays) {
    try {
      api = await initSqliteFromJson(jsonText, holidays);
      console.info(
        `[holidays-db] sqlite (${api.backend}), ${api.count()} rows`
      );
      return api;
    } catch (e) {
      console.warn("[holidays-db] WASM unavailable, JSON fallback:", e);
      api = makeJsonApi(holidays);
      return api;
    }
  }

  try {
    api = await initSqliteOfflineOnly();
    console.info(`[holidays-db] offline sqlite (${api.backend}), ${api.count()} rows`);
    return api;
  } catch (offlineErr) {
    console.error("[holidays-db] fetch failed and no cache:", fetchErr, offlineErr);
    throw fetchErr || offlineErr;
  }
}

export function getHolidaysApi() {
  if (!api) throw new Error("initHolidaysDb() not called");
  return api;
}
