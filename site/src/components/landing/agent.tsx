import { ArrowUpRight, Bot, Check, Code, LayoutDashboard, Terminal } from "lucide-react";
import Link from "next/link";

export function Agent() {
  return (
    <section id="agent" className="site-container product-section agent-section">
      <div className="agent-copy">
        <p className="section-kicker">Built for the way you work</p>
        <h2>
          Your platform.
          <br />
          Your agent.
          <br />
          Same possibilities.
        </h2>
        <p className="section-description">
          Click through the dashboard, script with the CLI, or connect your coding agent over MCP.
          Your infrastructure is ready for all three.
        </p>
        <div className="agent-channels">
          <span>
            <LayoutDashboard size={14} /> Dashboard
          </span>
          <span>
            <Terminal size={14} /> CLI
          </span>
          <span>
            <Code size={14} /> API
          </span>
          <span>
            <Bot size={14} /> MCP
          </span>
        </div>
        <Link className="inline-link" href="/docs/mcp">
          Connect your agent <ArrowUpRight size={16} />
        </Link>
      </div>
      <div>
        <div className="agent-console">
          <div className="agent-console-header">
            <span>
              <Bot size={15} /> Cubeship + your agent
            </span>
            <span>Example workflow</span>
          </div>
          <p className="agent-prompt">
            “Create a project, deploy my app and connect a Postgres database.”
          </p>
          <div className="agent-work">
            <div>
              <Check size={14} /> Create the project and environment
            </div>
            <div>
              <Check size={14} /> Provision and attach PostgreSQL
            </div>
            <div>
              <Check size={14} /> Deploy the application
            </div>
            <div>
              <Check size={14} /> Read the deployment result
            </div>
          </div>
          <div className="agent-result">
            <span className="status-light" /> From a conversation to a running app.
          </div>
        </div>
        <p className="agent-note">
          Authenticated with your API key. Resource ceilings and public ports stay in your hands.
        </p>
      </div>
    </section>
  );
}
