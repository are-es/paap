#!/bin/bash
# Fetch provider IDs from PAAP DB and generate static params
DB="$HOME/.paap/paap.db"
PROVIDERS_PAGE="/mnt/hdd/ares-workspace/paap/web/src/app/providers/[id]/page.tsx"

if [ ! -f "$DB" ]; then
  echo "DB not found at $DB"
  exit 1
fi

# Get provider IDs
IDS=$(sqlite3 "$DB" "SELECT id FROM providers;" 2>/dev/null)

if [ -z "$IDS" ]; then
  echo "No providers found"
  exit 1
fi

# Build the generateStaticParams array
PARAMS=""
while IFS= read -r id; do
  PARAMS="${PARAMS}    { id: \"$id\" },\n"
done <<< "$IDS"

# Write the page.tsx
cat > "$PROVIDERS_PAGE" << 'HEREDOC'
// Server component — exports generateStaticParams for static export
import { ProviderSetupClient } from "./client";

export function generateStaticParams() {
HEREDOC

echo "  return [" >> "$PROVIDERS_PAGE"
echo -e "$PARAMS" >> "$PROVIDERS_PAGE"
echo "  ];" >> "$PROVIDERS_PAGE"
echo "}" >> "$PROVIDERS_PAGE"

cat >> "$PROVIDERS_PAGE" << 'HEREDOC'

export default function ProviderPage() {
  return <ProviderSetupClient />;
}
HEREDOC

echo "Generated static params for providers: $(echo $IDS | tr '\n' ' ')"

# ── Groups ─────────────────────────────────────────────────
GROUPS_PAGE="/mnt/hdd/ares-workspace/paap/web/src/app/groups/[id]/page.tsx"

GROUP_IDS=$(sqlite3 "$DB" "SELECT id FROM groups;" 2>/dev/null)

if [ -n "$GROUP_IDS" ]; then
  GPARAMS=""
  while IFS= read -r gid; do
    GPARAMS="${GPARAMS}    { id: \"$gid\" },\\n"
  done <<< "$GROUP_IDS"

  cat > "$GROUPS_PAGE" << 'HEREDOC'
import { GroupSetupClient } from "./client";

export function generateStaticParams() {
HEREDOC
  echo "  return [" >> "$GROUPS_PAGE"
  echo -e "$GPARAMS" >> "$GROUPS_PAGE"
  echo "  ];" >> "$GROUPS_PAGE"
  echo "}" >> "$GROUPS_PAGE"
  cat >> "$GROUPS_PAGE" << 'HEREDOC'

export default function GroupPage() {
  return <GroupSetupClient />;
}
HEREDOC
  echo "Generated static params for groups: $(echo $GROUP_IDS | tr '\n' ' ')"
else
  echo "No groups found — skipping group static params"
fi
