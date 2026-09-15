import { ArrowDown, ArrowUpRight, Check } from "lucide-react";
import Link from "next/link";
import { InfrastructureScene } from "./infrastructure-scene";
import { ScenePoster } from "./scene-poster";

export function Hero() {
  return (
    <section className="hero-stage" id="product">
      <div className="site-container hero-layout">
        <div className="hero-copy">
          <p className="hero-intro">
            <span className="status-light" /> Open source. Self hosted. All yours.
          </p>
          <h1>
            Your infrastructure.
            <br />
            Ready to ship.
          </h1>
          <p className="hero-description">
            The power of a cloud platform.
            <br />
            The freedom of your own servers.
          </p>
          <p className="hero-detail">
            Deploy apps, run databases and keep everything under control. From your first VPS to
            your own cluster.
          </p>
          <div className="button-row">
            <Link
              className="button-primary"
              href="/docs/getting-started/install"
              data-track="install"
              data-track-location="hero"
            >
              Install Cubeship <ArrowUpRight size={18} />
            </Link>
            <a className="button-text" href="#screens">
              Explore the platform <ArrowDown size={16} />
            </a>
          </div>
          <div className="hero-assurances">
            <span>
              <Check size={13} /> Free & open source
            </span>
            <span>
              <Check size={13} /> No cloud account
            </span>
            <span>
              <Check size={13} /> Apache 2.0
            </span>
          </div>
        </div>
        <div className="hero-art">
          <InfrastructureScene variant="hero" fallback={<ScenePoster variant="hero" />} />
          <span className="scene-label scene-label-top">
            <span /> Your infrastructure, connected
          </span>
          <span className="scene-label scene-label-bottom">Built to run on your terms.</span>
        </div>
      </div>
      <div className="site-container hero-baseline">
        <span>From code to production. One platform.</span>
        <a href="#screens">
          Take a closer look <ArrowDown size={14} />
        </a>
      </div>
    </section>
  );
}
