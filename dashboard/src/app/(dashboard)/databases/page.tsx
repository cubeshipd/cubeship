"use client";

import { useQuery } from "@tanstack/react-query";
import { DatabaseIcon, PlusIcon } from "lucide-react";
import { useCreateDatabase } from "@/components/database-workspace";
import { ErrorAlert } from "@/components/error-alert";
import { useSession } from "@/components/session-context";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { can } from "@/lib/api";
import { datastoresQuery } from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";

export default function DatabasesPage() {
  const inventory = useQuery(datastoresQuery);
  const createDatabase = useCreateDatabase();
  const me = useSession();

  if (inventory.isError) return <ErrorAlert error={message(inventory.error)} />;
  if (inventory.isPending || (inventory.data?.length ?? 0) > 0) return null;

  return (
    <Card className="w-full flex-1 text-center">
      <CardContent className="flex h-full min-h-[24rem] flex-col items-center justify-center px-6 py-12">
        <span className="mb-5 flex size-14 items-center justify-center rounded-2xl border border-primary/20 bg-primary/5 text-primary">
          <DatabaseIcon className="size-7" />
        </span>
        <h1 className="text-xl font-medium tracking-tight">No databases yet</h1>
        <p className="mt-2 max-w-sm text-sm leading-6 text-muted-foreground">
          Create a database to give your apps a shared, managed data store.
        </p>
        {can(me, "databases", "manage") && (
          <Button className="mt-6" onClick={createDatabase}>
            <PlusIcon />
            Create your first database
          </Button>
        )}
      </CardContent>
    </Card>
  );
}
