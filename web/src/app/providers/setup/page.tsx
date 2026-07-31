import { Suspense } from "react";
import { ProviderSetupClient } from "./client";

export default function ProviderSetupPage() {
  return (
    <Suspense fallback={null}>
      <ProviderSetupClient />
    </Suspense>
  );
}
