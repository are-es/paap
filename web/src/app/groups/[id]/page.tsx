import { GroupSetupClient } from "./client";

export function generateStaticParams() {
  return [
    { id: "78d57b0041814759490bc285c82474e8" },
    { id: "c303a34770772270d42af6162ceda0ce" },
    { id: "claude-78d57b0041814759490bc285c82474e8" },
    { id: "claude-c303a34770772270d42af6162ceda0ce" },
    { id: "claude-500e97f856cf32666542641c1d94fa3f" },
    { id: "claude-936612f28b2a6c5ab4341495bd4634a7" },
    { id: "500e97f856cf32666542641c1d94fa3f" },
    { id: "936612f28b2a6c5ab4341495bd4634a7" },
  ];
}

export default function GroupPage() {
  return <GroupSetupClient />;
}
