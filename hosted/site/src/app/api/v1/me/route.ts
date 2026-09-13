import { currentUser } from "@/lib/auth/session";
import { json, route } from "@/lib/http";

// Never cached: it is the one answer that differs per request.
export const dynamic = "force-dynamic";

export const GET = route(async () => json({ user: await currentUser() }));
