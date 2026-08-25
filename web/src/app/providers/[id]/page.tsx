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
