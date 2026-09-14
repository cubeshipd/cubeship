import { SiteHeader } from "@/components/site-header";
export default function Layout({ children }: LayoutProps<"/">) {
  return (
    <>
      <SiteHeader />
      {children}
    </>
  );
}
