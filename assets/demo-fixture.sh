#!/usr/bin/env bash
# Builds everything assets/demo.tape records: a scratch repository, an isolated
# differ config, and a tmux session laid out the way the README describes —
# an agent on the left, differ on the right.
#
# Committed so the demo is reproducible rather than a lucky take: the same
# files, the same diff, the same line under the cursor, and the same delivery
# target. HOME is pointed inside the scratch directory so the recording cannot
# pick up the recorder's own theme or feedback_target — and cannot write to
# their real config.
set -euo pipefail

DIR=${1:-/tmp/differ-demo}
SESSION=${2:-differ-demo}

# The repository is a subdirectory, so the fixture's own files — the agent
# script and the isolated config — cannot show up in the changeset being
# reviewed. They did: the demo opened on five changed files instead of three.
REPO="$DIR/repo"
rm -rf "$DIR"
mkdir -p "$REPO/src" "$DIR/home/.config/differ"
cd "$REPO"

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

# What the agent then did — the diff the recording shows. src/session.ts now
# swallows the read error and returns null, which is the bug the demo comments
# on.
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

# The pane that stands in for the agent. It prints what it is given, which is
# what makes the send visible in the recording.
cat > "$DIR/agent.sh" <<'EOF'
#!/usr/bin/env bash
printf '\033[1magent\033[0m — waiting for review feedback\n\n'
while IFS= read -r line; do printf '%s\n' "$line"; done
EOF
chmod +x "$DIR/agent.sh"

tmux kill-session -t "$SESSION" 2>/dev/null || true
tmux new-session -d -s "$SESSION" -x 200 -y 44 -c "$REPO" \
  -e "HOME=$DIR/home" "$DIR/agent.sh"
AGENT_PANE=$(tmux list-panes -t "$SESSION" -F '#{pane_id}')
tmux split-window -h -t "$SESSION" -c "$REPO" -e "HOME=$DIR/home"

# feedback_target: tmux, aimed at the agent pane by id. Written into the
# session's own HOME, so the recorder's config is neither read nor touched.
cat > "$DIR/home/.config/differ/config.json" <<EOF
{
  "theme": "mocha",
  "feedback_target": "tmux",
  "tmux_target": "$AGENT_PANE",
  "split_diff": false
}
EOF

echo "fixture ready:"
echo "  repo    $REPO"
echo "  session $SESSION (agent pane $AGENT_PANE)"
echo "  config  $DIR/home/.config/differ/config.json"
