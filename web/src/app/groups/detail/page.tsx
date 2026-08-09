import { Suspense } from "react";
import { GroupSetupClient } from "../[id]/client";

export default function GroupDetailPage() {
  return (
    <Suspense fallback={null}>
      <GroupSetupClient />
    </Suspense>
  );
}
