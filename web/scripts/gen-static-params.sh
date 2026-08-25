#!/bin/bash
# Pre-build script: Generate static params for all provider pages
# Creates [id]/page.tsx files for each configured provider

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
PROVIDERS_PAGE="$WEB_DIR/src/app/providers/[id]/page.tsx"
GROUPS_PAGE="$WEB_DIR/src/app/groups/[id]/page.tsx"

echo "Generating static params for provider pages..."

# Ensure the [id] directory exists
mkdir -p "$(dirname "$PROVIDERS_PAGE")"
mkdir -p "$(dirname "$GROUPS_PAGE")"

# Generate providers page with proper static params
cat > "$PROVIDERS_PAGE" << 'EOF'
import { redirect } from "next/navigation";

export async function generateStaticParams() {
  return [{ id: "builtin-anigravity" }, { id: "1" }];
}

interface PageProps {
  params: Promise<{ id: string }>;
}

export default async function ProviderPage({ params }: PageProps) {
  const { id } = await params;
  redirect(`/providers/setup?id=${id}`);
}
EOF

echo "Generated providers/[id]/page.tsx with static params"

# Generate groups page with proper static params
cat > "$GROUPS_PAGE" << 'EOF'
import { redirect } from "next/navigation";

export async function generateStaticParams() {
  return [{ id: "1" }];
}

interface PageProps {
  params: Promise<{ id: string }>;
}

export default async function GroupPage({ params }: PageProps) {
  const { id } = await params;
  redirect(`/groups/detail?id=${id}`);
}
EOF

echo "Generated groups/[id]/page.tsx with static params"
