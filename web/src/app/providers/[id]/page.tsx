// Server component — exports generateStaticParams for static export
import { ProviderSetupClient } from "./client";

export function generateStaticParams() {
  return [
    { id: "builtin-anigravity" },
    { id: "builtin-google" },
    { id: "builtin-grok-cli" },
    { id: "builtin-kimchi" },
    { id: "builtin-meta" },
    { id: "builtin-ollamacloud" },
    { id: "builtin-openrouter" },
    { id: "builtin-xiaomi" },
    { id: "grok-cli" },
    { id: "meta-default" },
    { id: "tokengo-default" },

  ];
}

export default function ProviderPage() {
  return <ProviderSetupClient />;
}
