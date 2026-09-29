#!/usr/bin/env bash
# Builds the scratch repository assets/demo.tape records against.
#
# Committed so the demo is reproducible rather than a lucky take: same files,
# same diff, same line under the cursor.
set -euo pipefail

DIR=${1:-/tmp/differ-demo}
rm -rf "$DIR"
mkdir -p "$DIR/src"
cd "$DIR"
git init -q .
git config user.name "demo"
git config user.email "demo@example.com"

cat > src/session.ts <<'EOF'
import { readFile } from "node:fs/promises";

export type Session = { id: string; user: string; expiresAt: number };

export async function loadSession(path: string): Promise<Session | null> {
  const raw = await readFile(path, "utf8");
  return JSON.parse(raw) as Session;
}

export function isExpired(s: Session, now: number): boolean {
  return s.expiresAt < now;
}
EOF

cat > src/cache.ts <<'EOF'
const store = new Map<string, unknown>();

export function get(key: string): unknown {
  return store.get(key);
}

export function set(key: string, value: unknown): void {
  store.set(key, value);
}
EOF

git add -A
git commit -qm "feat: session loading and a small cache"

# What the agent then did — the diff the recording shows.
cat > src/session.ts <<'EOF'
import { readFile } from "node:fs/promises";

export type Session = { id: string; user: string; expiresAt: number };

export async function loadSession(path: string): Promise<Session | null> {
  try {
    const raw = await readFile(path, "utf8");
    return JSON.parse(raw) as Session;
  } catch {
    return null;
  }
}

export function isExpired(s: Session, now: number): boolean {
  return s.expiresAt <= now;
}
EOF

cat > src/cache.ts <<'EOF'
const store = new Map<string, { value: unknown; expiresAt: number }>();

export function get(key: string): unknown {
  const hit = store.get(key);
  if (!hit) return undefined;
  if (hit.expiresAt < Date.now()) {
    store.delete(key);
    return undefined;
  }
  return hit.value;
}

export function set(key: string, value: unknown, ttlMs = 60_000): void {
  store.set(key, { value, expiresAt: Date.now() + ttlMs });
}
EOF

cat > src/auth.ts <<'EOF'
import { loadSession, isExpired } from "./session";

export async function authorize(path: string, now: number) {
  const session = await loadSession(path);
  if (!session) return false;
  return !isExpired(session, now);
}
EOF

echo "fixture ready in $DIR"
