import { ArrowUpRight } from "lucide-react";
import Link from "next/link";
import { InfrastructureScene } from "./infrastructure-scene";
import { ScenePoster } from "./scene-poster";

export function Cluster() {
  return (
    <section id="cluster" className="cluster-section site-container">
      <div className="section-heading">
        <div>
          <p className="section-kicker">Space to grow</p>
          <h2>
            Start with a server.
            <br />
            Build your own cloud.
          </h2>
        </div>
        <div>
          <p className="section-description">
            Add another machine when you need it. Your apps get more room. You keep one platform.
          </p>
          <Link href="/docs/servers" className="inline-link">
            Meet the cluster <ArrowUpRight size={16} />
          </Link>
        </div>
      </div>
      <div className="cluster-art">
        <InfrastructureScene variant="cluster" fallback={<ScenePoster variant="cluster" />} />
        <div className="cluster-annotations" aria-hidden="true">
          <span>Your servers</span>
          <span>Private connections</span>
          <span>One platform</span>
        </div>
      </div>
      <div className="cluster-details">
        <div>
          <h3>One front door.</h3>
          <p>
            Domains and certificates stay at your control plane. Moving an app between machines
            keeps its address intact.
          </p>
        </div>
        <div>
          <h3>Connected by design.</h3>
          <p>
            An encrypted private network connects your machines. Services reach each other by the
            names they already use.
          </p>
        </div>
        <div>
          <h3>Scale on your terms.</h3>
          <p>
            Place apps where they belong, add replicas or set a CPU target and let Cubeship adjust
            the count.
          </p>
        </div>
      </div>
    </section>
  );
}
