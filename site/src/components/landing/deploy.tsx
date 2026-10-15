import { ArrowUpRight, Box, Check, GitBranch, Globe, Terminal } from "lucide-react";
import Link from "next/link";

export function Deploy() {
  return (
    <section id="deploy" className="site-container product-section deploy-section">
      <div className="deploy-copy">
        <p className="section-kicker">Made for shipping</p>
        <h2>
          Great ideas belong
          <br />
          in production.
        </h2>
        <p className="section-description">
          Bring your code. Cubeship handles the build, the container and the route to the world.
        </p>
        <Link href="/docs/apps" className="inline-link">
          Find your deployment flow <ArrowUpRight size={16} />
        </Link>
      </div>
      <div className="deploy-visual">
        <div className="deploy-sources">
          <div>
            <GitBranch size={18} />
            <span>Git repository</span>
          </div>
          <div>
            <Box size={18} />
            <span>Docker image</span>
          </div>
          <div>
            <Terminal size={18} />
            <span>Registry push</span>
          </div>
        </div>
        <div className="deploy-connector" aria-hidden="true">
          <span />
          <span />
          <span />
        </div>
        <div className="deploy-log">
          <div className="deploy-log-heading">
            <span>
              <GitBranch size={14} /> main
            </span>
            <span className="deploy-badge">Deployment flow</span>
          </div>
          <div className="deploy-log-line">
            <Check size={14} />
            <span>Build your application</span>
            <span>Build</span>
          </div>
          <div className="deploy-log-line">
            <Check size={14} />
            <span>Start the new container</span>
            <span>Run</span>
          </div>
          <div className="deploy-log-line">
            <Check size={14} />
            <span>Wait for a healthy response</span>
            <span>Check</span>
          </div>
          <div className="deploy-live">
            <Globe size={17} />
            <span>Your app is live.</span>
            <span className="status-light" />
          </div>
        </div>
      </div>
      <div className="deploy-notes">
        <div>
          <strong>Push to deploy</strong>
          <p>Connect GitHub and let each push move your app forward.</p>
        </div>
        <div>
          <strong>Your build, your server</strong>
          <p>Use a Dockerfile, or let Railpack build from your repository.</p>
        </div>
        <div>
          <strong>Health before handover</strong>
          <p>The new container must be healthy before the old one goes.</p>
        </div>
      </div>
    </section>
  );
}
