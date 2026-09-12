import { HomeLayout } from "fumadocs-ui/layouts/home";
import { Footer } from "@/components/landing/footer";
import { baseOptions } from "@/lib/layout.shared";

export default function Layout({ children }: LayoutProps<"/templates">) {
  return (
    <HomeLayout {...baseOptions()}>
      <main className="flex-1">{children}</main>
      <Footer />
    </HomeLayout>
  );
}
