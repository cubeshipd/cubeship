"use client";

import { useQuery } from "@tanstack/react-query";
import { HardDriveIcon, PlusIcon } from "lucide-react";
import { useCreateStorage } from "@/components/storage-workspace";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { storesQuery } from "@/lib/dashboard-queries";

export default function StoragePage() {
  const inventory = useQuery(storesQuery);
  const create = useCreateStorage();
  if (inventory.isError || inventory.isPending || (inventory.data?.length ?? 0) > 0) return null;

  return (
    <Card className="w-full flex-1 text-center">
      <CardContent className="flex h-full min-h-[24rem] flex-col items-center justify-center px-6 py-12">
        <span className="mb-5 flex size-14 items-center justify-center rounded-2xl border border-primary/20 bg-primary/5 text-primary">
          <HardDriveIcon className="size-7" />
        </span>
        <h1 className="text-xl font-medium tracking-tight">No object storage yet</h1>
        <p className="mt-2 max-w-sm text-sm leading-6 text-muted-foreground">
          Connect an S3 bucket or run managed storage for your apps and backups.
        </p>
        <Button className="mt-6" onClick={create}>
          <PlusIcon />
          Add your first storage
        </Button>
      </CardContent>
    </Card>
  );
}
