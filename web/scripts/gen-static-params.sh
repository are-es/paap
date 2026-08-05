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
import { getProviders, type Provider } from "@/lib/providers";
import { redirect } from "next/navigation";
import { headers } from "next/headers";

// Generate static params for all providers
export async function generateStaticParams() {
  try {
    const providers = await getProviders();
    return providers.map((provider) => ({
      id: provider.id,
    }));
  } catch (error) {
    console.error("Failed to generate static params:", error);
    return [];
  }
}

interface PageProps {
  params: Promise<{ id: string }>;
}

export default async function ProviderPage({ params }: PageProps) {
  const { id } = await params;
  const headersList = await headers();
  const host = headersList.get("host") || "";
  const protocol = headersList.get("x-forwarded-proto") || "http";
  const baseUrl = `${protocol}://${host}`;

  try {
    const providers = await getProviders();
    const provider = providers.find((p) => p.id === id);

    if (!provider) {
      redirect("/providers");
    }

    // Redirect to setup page with provider data
    const setupUrl = new URL("/providers/setup", baseUrl);
    setupUrl.searchParams.set("id", provider.id);
    setupUrl.searchParams.set("provider", provider.name);
    setupUrl.searchParams.set("type", provider.type);
    setupUrl.searchParams.set("config", encodeURIComponent(JSON.stringify(provider.config)));
    setupUrl.searchParams.set("apiUrl", provider.apiUrl || "");
    setupUrl.searchParams.set("isActive", provider.isActive.toString());
    setupUrl.searchParams.set("isEditing", "true");

    redirect(setupUrl.pathname + setupUrl.search);
  } catch (error) {
    console.error("Failed to load provider:", error);
    redirect("/providers");
  }
}
EOF

echo "Generated providers/[id]/page.tsx with static params"

# Generate groups page with proper static params
cat > "$GROUPS_PAGE" << 'EOF'
import { getGroups } from "@/lib/api";
import { redirect } from "next/navigation";
import { headers } from "next/headers";

// Generate static params for all groups
export async function generateStaticParams() {
  try {
    const groups = await getGroups();
    return groups.map((group) => ({
      id: group.id,
    }));
  } catch (error) {
    console.error("Failed to generate static params:", error);
    return [];
  }
}

interface PageProps {
  params: Promise<{ id: string }>;
}

export default async function GroupPage({ params }: PageProps) {
  const { id } = await params;
  const headersList = await headers();
  const host = headersList.get("host") || "";
  const protocol = headersList.get("x-forwarded-proto") || "http";
  const baseUrl = `${protocol}://${host}`;

  try {
    const groups = await getGroups();
    const group = groups.find((g) => g.id === id);

    if (!group) {
      redirect("/groups");
    }

    // Redirect to setup page with group data
    const setupUrl = new URL("/groups/setup", baseUrl);
    setupUrl.searchParams.set("id", group.id);
    setupUrl.searchParams.set("name", group.name);
    setupUrl.searchParams.set("description", group.description || "");
    setupUrl.searchParams.set("isEditing", "true");

    redirect(setupUrl.pathname + setupUrl.search);
  } catch (error) {
    console.error("Failed to load group:", error);
    redirect("/groups");
  }
}
EOF

echo "Generated groups/[id]/page.tsx with static params"
