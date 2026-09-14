import { ArrowUpRight, ShieldCheck } from "lucide-react";
import Link from "next/link";
import { InstallCommand } from "@/components/install-command";

export function Privacy() {
  return (
    <section id="install" className="install-section">
      <div className="site-container install-layout">
        <div>
          <h2>
            Your next deploy.
            <br />
            On your terms.
          </h2>
          <p>
            One command turns your server into your platform. Free and open source, with every
            feature included.
          </p>
          <div className="ownership-line">
            <ShieldCheck size={17} />
            <Link href="/docs/operating/what-reaches-the-internet">
              No telemetry in your instance. Your data stays yours.
            </Link>
          </div>
        </div>
        <div>
          <div className="install-caption">A fresh server. One command. Let's ship.</div>
          <InstallCommand />
          <div className="install-requirements">
            <span>Debian or Ubuntu · x86-64 or arm64</span>
            <Link href="/docs/getting-started/install">
              Installation guide <ArrowUpRight className="inline" size={12} />
            </Link>
          </div>
        </div>
      </div>
    </section>
  );
}
