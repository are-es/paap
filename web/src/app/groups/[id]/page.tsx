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
