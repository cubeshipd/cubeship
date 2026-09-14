import { Footer } from "@/components/landing/footer";
import { SiteHeader } from "@/components/site-header";

export default function Layout({ children }: LayoutProps<"/templates">) {
  return (
    <>
      <SiteHeader />
      <main id="main-content" className="editorial-page templates-shell flex-1">
        {children}
      </main>
      <Footer />
    </>
  );
}
