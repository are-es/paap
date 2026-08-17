"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { GroupSetupClient } from "../[id]/client";

function GroupDetailInner() {
  const searchParams = useSearchParams();
  const id = searchParams.get("id") ?? "new";
  return <GroupSetupClient groupId={id} />;
}

export default function GroupDetailPage() {
  return (
    <Suspense fallback={null}>
      <GroupDetailInner />
    </Suspense>
  );
}
