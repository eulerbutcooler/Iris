"use client";

import { Workflow, Plus, Play, Square, Trash2, Loader2, X, ChevronLeft, Save, FileText, Copy, CheckCircle, Link } from "lucide-react";
import { useEffect, useState, useCallback } from "react";
import * as api from "@/lib/api";
import { WorkflowCanvas } from "@/components/workflow/WorkflowCanvas";
import { RelayLogs } from "@/components/workflow/RelayLogs";
import { flowToRelay } from "@/lib/workflow/converters";

type ViewMode = "list" | "create" | "edit" | "logs";

export default function RelaysPage() {
  const [mode, setMode] = useState<ViewMode>("list");
  const [relays, setRelays] = useState<api.Relay[]>([]);
  const [editRelay, setEditRelay] = useState<api.RelayWithActions | null>(null);
  const [logsRelay, setLogsRelay] = useState<api.Relay | null>(null);
  const [secrets, setSecrets] = useState<api.Secret[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  // Relay name + desc prompt for create mode
  const [relayName, setRelayName] = useState("");
  const [relayDesc, setRelayDesc] = useState("");
  const [showNameModal, setShowNameModal] = useState(false);
  const [pendingSave, setPendingSave] = useState<ReturnType<typeof flowToRelay> | null>(null);

  async function loadData() {
    try {
      const [r, s] = await Promise.all([api.getRelays(), api.getSecrets()]);
      setRelays(r);
      setSecrets(s);
    } catch {
      // silent
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadData();
    // Reload when AIChat deploys or updates a relay
    const onRelayChanged = () => loadData();
    window.addEventListener("iris:relay-changed", onRelayChanged);
    return () => window.removeEventListener("iris:relay-changed", onRelayChanged);
  }, []);

  // ── Canvas save handler ────────────────────────────────────────────────────
  const handleCanvasSave = useCallback(
    (data: ReturnType<typeof flowToRelay>) => {
      if (mode === "create") {
        setPendingSave(data);
        setShowNameModal(true);
      } else if (mode === "edit" && editRelay) {
        doUpdateRelay(editRelay.id, data);
      }
    },
    [mode, editRelay],
  );

  async function doCreateRelay(data: ReturnType<typeof flowToRelay>, name: string, desc: string) {
    setSaving(true);
    try {
      await api.createRelay({
        name,
        description: desc,
        trigger_type: data.triggerType,
        trigger_config: data.triggerConfig,
        actions: data.actions,
        edges: data.relayEdges,
      });
      setShowNameModal(false);
      setRelayName("");
      setRelayDesc("");
      setPendingSave(null);
      setMode("list");
      await loadData();
    } catch (e) {
      alert(e instanceof Error ? e.message : "Failed to create relay");
    } finally {
      setSaving(false);
    }
  }

  async function doUpdateRelay(relayId: string, data: ReturnType<typeof flowToRelay>) {
    setSaving(true);
    try {
      await api.updateRelay(relayId, {
        trigger_type: data.triggerType,
        trigger_config: data.triggerConfig,
      });
      await api.updateRelayActions(relayId, data.actions, data.relayEdges);
      setMode("list");
      setEditRelay(null);
      await loadData();
    } catch (e) {
      alert(e instanceof Error ? e.message : "Failed to save relay");
    } finally {
      setSaving(false);
    }
  }

  async function openEdit(relay: api.Relay) {
    try {
      const full = await api.getRelay(relay.id);
      setEditRelay(full);
      setMode("edit");
    } catch { /* silent */ }
  }

  function openLogs(relay: api.Relay) {
    setLogsRelay(relay);
    setMode("logs");
  }

  async function handleToggle(relay: api.Relay) {
    try {
      await api.updateRelay(relay.id, { name: relay.name, is_active: !relay.is_active });
      await loadData();
    } catch { /* silent */ }
  }

  async function handleDelete(id: string) {
    try {
      await api.deleteRelay(id);
      await loadData();
    } catch { /* silent */ }
  }

  async function handleTrigger(id: string) {
    try {
      await api.triggerRelay(id);
    } catch { /* silent */ }
  }

  // ── Logs view ─────────────────────────────────────────────────────────────
  if (mode === "logs" && logsRelay) {
    return (
      <div className="h-full">
        <RelayLogs relay={logsRelay} onBack={() => { setMode("list"); setLogsRelay(null); }} />
      </div>
    );
  }

  // ── Canvas editor view ────────────────────────────────────────────────────
  if (mode === "create" || mode === "edit") {
    return (
      <div className="flex flex-col h-full animate-in fade-in duration-300">
        {/* Editor header */}
        <div className="flex items-center justify-between border-b border-iris-border-strong pb-4 mb-4 shrink-0">
          <div className="flex items-center gap-4">
            <button
              onClick={() => { setMode("list"); setEditRelay(null); }}
              className="flex items-center gap-2 text-iris-secondary hover:text-white text-xs font-bold uppercase tracking-widest transition-colors"
            >
              <ChevronLeft className="w-4 h-4" /> Back
            </button>
            <div className="w-px h-5 bg-iris-border-strong" />
            <div>
              <h1 className="text-lg font-black tracking-widest text-white uppercase flex items-center gap-2">
                <Workflow className="w-5 h-5 text-iris-accent" />
                {mode === "create" ? "New Relay" : `Editing: ${editRelay?.name}`}
              </h1>
              <p className="text-xs text-iris-secondary font-mono mt-0.5">
                {mode === "create"
                  ? "Drag nodes from the palette → connect them → click Save Graph"
                  : "Modify the workflow then click Save Graph"}
              </p>
            </div>
          </div>
          {saving && (
            <div className="flex items-center gap-2 text-iris-accent text-xs font-bold uppercase tracking-widest">
              <Loader2 className="w-4 h-4 animate-spin" /> Saving…
            </div>
          )}
        </div>

        <div className="flex-1 min-h-0">
          <WorkflowCanvas
            relay={editRelay ?? undefined}
            secrets={secrets}
            onSave={handleCanvasSave}
            height="100%"
          />
        </div>

        {/* Name modal */}
        {showNameModal && (
          <div className="fixed inset-0 bg-black/70 backdrop-blur-sm z-50 flex items-center justify-center">
            <div className="bg-iris-surface border border-iris-border-strong w-full max-w-sm p-6 space-y-5">
              <div className="flex items-center justify-between">
                <h2 className="text-sm font-black text-white uppercase tracking-widest">Deploy Relay</h2>
                <button onClick={() => setShowNameModal(false)}>
                  <X className="w-4 h-4 text-iris-secondary hover:text-white" />
                </button>
              </div>
              <div className="space-y-3">
                <input
                  autoFocus
                  type="text"
                  value={relayName}
                  onChange={(e) => setRelayName(e.target.value)}
                  placeholder="Relay name…"
                  className="w-full bg-iris-base border border-iris-border-strong px-4 py-3 text-white text-sm font-mono focus:outline-none focus:border-iris-accent transition-colors placeholder:text-iris-muted"
                />
                <input
                  type="text"
                  value={relayDesc}
                  onChange={(e) => setRelayDesc(e.target.value)}
                  placeholder="Description (optional)…"
                  className="w-full bg-iris-base border border-iris-border-strong px-4 py-3 text-white text-sm font-mono focus:outline-none focus:border-iris-accent transition-colors placeholder:text-iris-muted"
                />
              </div>
              <button
                onClick={() => pendingSave && doCreateRelay(pendingSave, relayName, relayDesc)}
                disabled={!relayName.trim() || saving}
                className="w-full bg-iris-accent text-black font-black text-sm tracking-widest uppercase py-3 flex items-center justify-center gap-2 hover:bg-white transition-colors disabled:opacity-40"
              >
                {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
                Deploy
              </button>
            </div>
          </div>
        )}
      </div>
    );
  }

  // ── List view ─────────────────────────────────────────────────────────────
  return (
    <div className="space-y-6 animate-in fade-in duration-500">
      <div className="flex justify-between items-center border-b border-iris-border-strong pb-4">
        <div>
          <h1 className="text-xl font-black tracking-widest text-white uppercase flex items-center gap-3">
            <Workflow className="w-5 h-5 text-iris-accent" />
            Relay Matrix
          </h1>
          <p className="text-xs text-iris-secondary font-mono mt-1">
            Design and configure autonomous node sequences — click a relay to view logs
          </p>
        </div>
        <button
          onClick={() => setMode("create")}
          className="bg-iris-accent/10 text-iris-accent border border-iris-accent px-4 py-2 text-xs font-bold tracking-widest uppercase hover:bg-iris-accent hover:text-black transition-colors flex items-center gap-2"
        >
          <Plus className="w-4 h-4" /> New Relay
        </button>
      </div>

      {loading ? (
        <div className="flex items-center gap-3 text-iris-secondary text-sm py-12 justify-center">
          <Loader2 className="w-5 h-5 animate-spin" /> Loading relays…
        </div>
      ) : relays.length === 0 ? (
        <div className="text-center py-16 border border-iris-border-strong bg-iris-surface">
          <Workflow className="w-12 h-12 text-iris-border-strong mx-auto mb-4" />
          <p className="text-sm text-iris-secondary mb-6">No relays deployed.</p>
          <button
            onClick={() => setMode("create")}
            className="bg-iris-accent text-black font-black text-xs tracking-widest uppercase px-6 py-3 hover:bg-white transition-colors flex items-center gap-2 mx-auto"
          >
            <Plus className="w-4 h-4" /> Create Your First Relay
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {relays.map((relay) => (
            <RelayCard
              key={relay.id}
              relay={relay}
              onViewLogs={() => openLogs(relay)}
              onEdit={() => openEdit(relay)}
              onToggle={() => handleToggle(relay)}
              onTrigger={() => handleTrigger(relay.id)}
              onDelete={() => handleDelete(relay.id)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function RelayCard({
  relay,
  onViewLogs,
  onEdit,
  onToggle,
  onTrigger,
  onDelete,
}: {
  relay: api.Relay;
  onViewLogs: () => void;
  onEdit: () => void;
  onToggle: () => void;
  onTrigger: () => void;
  onDelete: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const status = relay.is_active ? "ACTIVE" : "IDLE";
  const statusColors: Record<string, string> = {
    ACTIVE: "text-iris-success bg-iris-success/10 border-iris-success/30",
    IDLE: "text-iris-warning bg-iris-warning/10 border-iris-warning/30",
  };

  const isWebhook = relay.trigger_type === "webhook";
  const hooksBase = process.env.NEXT_PUBLIC_HOOKS_URL ?? "http://localhost:8080";
  const webhookUrl = isWebhook ? `${hooksBase}/hooks/${relay.id}` : null;

  function copyUrl(e: React.MouseEvent) {
    e.stopPropagation();
    if (!webhookUrl) return;
    navigator.clipboard?.writeText(webhookUrl);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  return (
    <div
      onClick={onViewLogs}
      className="border border-iris-border-strong bg-iris-surface p-5 relative group hover:border-iris-accent/50 transition-colors cursor-pointer"
    >
      {/* Delete button — stop propagation so card click doesn't fire */}
      <button
        onClick={(e) => { e.stopPropagation(); onDelete(); }}
        className="absolute top-0 right-0 w-8 h-8 flex items-center justify-center border-l border-b border-iris-border-strong bg-iris-base text-iris-secondary group-hover:text-iris-error transition-colors"
      >
        <Trash2 className="w-3.5 h-3.5" />
      </button>

      <div className="mb-3">
        <div className={`inline-flex items-center gap-1.5 px-2 py-0.5 text-[9px] font-black tracking-widest border ${statusColors[status]}`}>
          <div className={`w-1.5 h-1.5 rounded-full ${status === "ACTIVE" ? "bg-iris-success animate-pulse" : "bg-iris-warning"}`} />
          {status}
        </div>
      </div>

      <h3 className="font-mono font-bold text-white text-lg mb-1 truncate pr-8" title={relay.name}>{relay.name}</h3>
      {relay.description && (
        <p className="text-xs text-iris-secondary mb-3 truncate">{relay.description}</p>
      )}

      <div className="space-y-1.5 mb-4 text-xs font-mono text-iris-secondary">
        <div className="flex justify-between">
          <span className="uppercase opacity-50">Trigger</span>
          <span className="text-white uppercase">{relay.trigger_type}</span>
        </div>
        {relay.next_run_at && (
          <div className="flex justify-between">
            <span className="uppercase opacity-50">Next Run</span>
            <span className="text-iris-accent-sub">
              {new Date(relay.next_run_at).toLocaleString("en-GB", { hour12: false })}
            </span>
          </div>
        )}
        <div className="flex justify-between">
          <span className="uppercase opacity-50">Updated</span>
          <span className="text-white">{new Date(relay.updated_at).toLocaleDateString()}</span>
        </div>
      </div>

      {/* Webhook URL — only for webhook-triggered relays */}
      {isWebhook && webhookUrl && (
        <div
          className="mb-4 border border-iris-accent/20 bg-iris-accent/5 p-3 space-y-2"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-1.5 text-[9px] font-black tracking-widest text-iris-accent uppercase">
              <Link className="w-3 h-3" />
              Webhook URL
            </div>
            <button
              onClick={copyUrl}
              className="flex items-center gap-1 text-[9px] font-bold uppercase tracking-widest px-2 py-0.5 border transition-colors"
              style={{
                color: copied ? "var(--iris-success)" : "var(--iris-accent-core)",
                borderColor: copied ? "rgba(16,185,129,0.3)" : "rgba(16,185,129,0.3)",
                background: copied ? "rgba(16,185,129,0.1)" : "transparent",
              }}
            >
              {copied ? <CheckCircle className="w-3 h-3" /> : <Copy className="w-3 h-3" />}
              {copied ? "Copied!" : "Copy"}
            </button>
          </div>
          <code className="block text-[10px] font-mono text-iris-accent break-all leading-relaxed select-all">
            {webhookUrl}
          </code>
          <div className="text-[9px] text-iris-muted leading-relaxed">
            Send a POST request to this URL to trigger the relay.
          </div>
        </div>
      )}

      {/* Hint */}
      <div className="text-[10px] text-iris-border-strong group-hover:text-iris-accent transition-colors mb-3 flex items-center gap-1">
        <FileText className="w-3 h-3" /> Click to view execution logs
      </div>

      <div
        className="flex gap-2 border-t border-iris-border-strong pt-4"
        onClick={(e) => e.stopPropagation()}
      >
        <button
          onClick={onEdit}
          className="flex-1 flex items-center justify-center gap-1.5 p-2 bg-iris-base border border-iris-border-strong text-xs font-bold text-white hover:border-iris-accent hover:text-iris-accent transition-colors"
        >
          <Workflow className="w-3 h-3" /> Edit
        </button>
        <button
          onClick={onTrigger}
          className="flex items-center justify-center gap-1.5 px-3 p-2 bg-iris-base border border-iris-border-strong text-xs font-bold text-white hover:border-iris-success hover:text-iris-success transition-colors"
          title="Run now"
        >
          <Play className="w-3 h-3" />
        </button>
        <button
          onClick={onToggle}
          className="flex items-center justify-center gap-1.5 px-3 p-2 bg-iris-base border border-iris-border-strong text-xs font-bold text-white hover:border-iris-warning hover:text-iris-warning transition-colors"
          title={relay.is_active ? "Pause" : "Activate"}
        >
          <Square className="w-3 h-3" />
        </button>
      </div>
    </div>
  );
}

