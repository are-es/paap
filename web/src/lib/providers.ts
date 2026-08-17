// Re-export provider utilities for server-side static generation
import { api, type Provider } from "./api";

export type { Provider };

export async function getProviders(): Promise<Provider[]> {
  return api.getProviders();
}
