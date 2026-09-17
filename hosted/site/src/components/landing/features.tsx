import {
  Activity,
  Archive,
  ArrowUpRight,
  Database,
  HardDrive,
  LockKeyhole,
  Network,
  Shield,
  SquareTerminal,
} from "lucide-react";
import Link from "next/link";

export function Features() {
  return (
    <section className="platform-section" id="platform">
      <div className="site-container product-section">
        <div className="section-heading">
          <div>
            <p className="section-kicker">More than a deploy button</p>
            <h2>
              A home for your
              <br />
              entire stack.
            </h2>
          </div>
          <p>
            The services your apps depend on.
            <br />
            Connected, visible and under your control.
          </p>
        </div>
        <div className="platform-grid">
          <article className="platform-databases">
            <div className="feature-icon">
              <Database size={23} />
            </div>
            <h3>
              Data, right where
              <br />
              you need it.
            </h3>
            <p>
              Provision a database and attach it to an app. Connection variables are wired in for
              you.
            </p>
            <div className="database-stack">
              <div>
                <Database size={20} />
                <strong>PostgreSQL</strong>
                <span>Relational</span>
              </div>
              <div>
                <Database size={20} />
                <strong>MySQL / MariaDB</strong>
                <span>Relational</span>
              </div>
              <div>
                <Database size={20} />
                <strong>MongoDB</strong>
                <span>Document</span>
              </div>
              <div>
                <Database size={20} />
                <strong>Redis</strong>
                <span>In-memory</span>
              </div>
            </div>
            <Link href="/docs/databases" className="inline-link">
              Explore databases <ArrowUpRight size={16} />
            </Link>
          </article>
          <article className="platform-backups">
            <Archive size={23} className="feature-icon" />
            <h3>
              A way back.
              <br />
              Built in.
            </h3>
            <p>
              Scheduled database backups to an off-server bucket. Restore from the same dashboard.
            </p>
            <div className="backup-visual" aria-hidden="true">
              <div className="backup-orbit">
                <Database size={25} />
              </div>
              <span />
              <div className="backup-orbit">
                <Archive size={25} />
              </div>
              <span />
              <div className="backup-orbit">
                <Shield size={25} />
              </div>
            </div>
            <Link href="/docs/backups" className="inline-link">
              Protect your data <ArrowUpRight size={16} />
            </Link>
          </article>
          <article className="platform-storage">
            <HardDrive size={23} className="feature-icon" />
            <h3>Room for everything.</h3>
            <p>Run MinIO on your server or connect the S3 storage you already use.</p>
            <Link href="/docs/object-storage" className="inline-link">
              Explore object storage <ArrowUpRight size={16} />
            </Link>
          </article>
        </div>
        <div className="platform-utilities">
          <div>
            <LockKeyhole size={20} />
            <span>
              <strong>Automatic HTTPS</strong>
              <small>Certificates that renew themselves.</small>
            </span>
          </div>
          <div>
            <Network size={20} />
            <span>
              <strong>DNS & networking</strong>
              <small>Connect your domains and services.</small>
            </span>
          </div>
          <div>
            <Shield size={20} />
            <span>
              <strong>Firewall controls</strong>
              <small>Choose what reaches the internet.</small>
            </span>
          </div>
          <div>
            <SquareTerminal size={20} />
            <span>
              <strong>Shell access</strong>
              <small>A terminal in any container or server.</small>
            </span>
          </div>
          <div>
            <Activity size={20} />
            <span>
              <strong>Live metrics</strong>
              <small>See what your containers are using.</small>
            </span>
          </div>
        </div>
      </div>
    </section>
  );
}
