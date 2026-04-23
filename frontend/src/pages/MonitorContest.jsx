import React, { useState, useEffect, useRef, useCallback } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import {
  FaUserShield, FaUsers, FaExclamationTriangle, FaCheckCircle,
  FaSatellite, FaGlobe, FaClock, FaWifi, FaSkull, FaBan,
  FaIdBadge, FaDatabase, FaFire, FaArrowLeft,
} from "react-icons/fa";
import styles from "./MonitorContest.module.css";


const API           = import.meta.env.VITE_API_URL;
const POLL_INTERVAL = 3000;

const MonitorContest = () => {
  const [tab, setTab]               = useState("monitor");
  const [teams, setTeams]           = useState([]);
  const [violations, setViolations] = useState([]);
  const [aiHits, setAiHits]         = useState([]);
  const [loading, setLoading]       = useState(true);
  const [error, setError]           = useState(null);
  const [lastSync, setLastSync]     = useState(null);
  const [contests, setContests]     = useState([]);
  const [pickerLoading, setPickerLoading] = useState(false);

  const intervalRef    = useRef(null);
  const prevViolations = useRef(0);
  const prevAiHits     = useRef(0);
  const audioRef       = useRef(null);

  const location  = useLocation();
  const navigate  = useNavigate();
  const contestId = new URLSearchParams(location.search).get("id");

  useEffect(() => {
    audioRef.current = new Audio("/notification.mp3");
    audioRef.current.volume = 1.0;
  }, []);

  const playAlert = useCallback(() => {
    if (!audioRef.current) return;
    audioRef.current.currentTime = 0;
    audioRef.current.play().catch(() => {});
  }, []);

  useEffect(() => {
    if (!contestId) {
      setPickerLoading(true);
      fetch(`${API}/contests`)
        .then((r) => r.json())
        .then((data) => setContests(Array.isArray(data) ? data : []))
        .catch(() => setContests([]))
        .finally(() => setPickerLoading(false));
    }
  }, [contestId]);

  const fetchTelemetry = useCallback(async () => {
    if (!contestId) return;
    try {
      const res = await fetch(`${API}/contests/${contestId}/monitor`, { cache: "no-store" });
      if (!res.ok) throw new Error(`Server returned ${res.status}`);
      const raw = await res.json();
      setTeams(Array.isArray(raw) ? raw : []);
      setError(null);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, [contestId]);

  const fetchViolations = useCallback(async () => {
    if (!contestId) return;
    try {
      const res = await fetch(`${API}/contests/${contestId}/violations`, { cache: "no-store" });
      if (!res.ok) return;
      const raw = await res.json();
      const data = Array.isArray(raw) ? raw : [];

      // Play sound if new violations appeared
      if (data.length > prevViolations.current) {
        playAlert();
      }
      prevViolations.current = data.length;

      setViolations(data);
      setLastSync(new Date());
    } catch (_) {}
  }, [contestId, playAlert]);

  const fetchAIHits = useCallback(async () => {
    if (!contestId) return;
    try {
      const res = await fetch(`${API}/contests/${contestId}/ai-hits`, { cache: "no-store" });
      if (!res.ok) return;
      const raw = await res.json();
      const data = Array.isArray(raw) ? raw : [];

      // Play sound if new AI hits appeared
      if (data.length > prevAiHits.current) {
        playAlert();
      }
      prevAiHits.current = data.length;

      setAiHits(data);
    } catch (_) {}
  }, [contestId, playAlert]);

  useEffect(() => {
    if (!contestId) return;
    fetchTelemetry();
    fetchViolations();
    fetchAIHits();
    intervalRef.current = setInterval(() => {
      fetchTelemetry();
      fetchViolations();
      fetchAIHits();
    }, POLL_INTERVAL);
    return () => clearInterval(intervalRef.current);
  }, [contestId, fetchTelemetry, fetchViolations, fetchAIHits]);

  const syncLabel = lastSync
    ? lastSync.toLocaleTimeString("en-BD", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })
    : "---";

  const violationCount = teams.filter((t) => t.is_warning === true || t.is_warning === 1).length;

  /* ── No ID → picker ── */
  if (!contestId) {
    return (
      <div className={styles.container}>
        <div className={styles.scanline} />
        <header className={styles.header}>
          <div className={styles.headerInfo}>
            <FaSatellite className={styles.mainIcon} />
            <div>
              <h1 className={styles.title}>Live Contest Monitor<span className={styles.cursor}>_</span></h1>
              <p className={styles.subtext}>// select a contest to open its telemetry feed</p>
            </div>
          </div>
        </header>

        {pickerLoading ? (
          <div className={styles.loadingText}>&gt; Loading contests...</div>
        ) : contests.length === 0 ? (
          <div className={styles.empty}>
            <FaGlobe className={styles.emptyIcon} />
            <p>No contests found. Host one first from the Dashboard.</p>
          </div>
        ) : (
          <div className={styles.pickerGrid}>
            {contests.map((c) => (
              <button
                key={c.id}
                className={styles.pickerCard}
                onClick={() => navigate(`/monitor-contest?id=${c.id}`)}
              >
                <FaSatellite className={styles.pickerIcon} />
                <span className={styles.pickerName}>{c.name}</span>
                <span className={styles.pickerSub}>Contest #{c.id} — click to monitor</span>
              </button>
            ))}
          </div>
        )}
      </div>
    );
  }

  /* ── Monitor view ── */
  return (
    <div className={styles.container}>
      <div className={styles.scanline} />

      {/* Header */}
      <header className={styles.header}>
        <div className={styles.headerInfo}>
          <FaSatellite className={styles.mainIcon} />
          <div>
            <h1 className={styles.title}>Live Contest Monitor<span className={styles.cursor}>_</span></h1>
            <p className={styles.subtext}>
              Teams:&nbsp;<span className={styles.count}>{teams.length}</span>
              &ensp;|&ensp;Violations:&nbsp;
              <span className={violationCount > 0 ? styles.danger : styles.count}>{violationCount}</span>
              &ensp;|&ensp;AI Hits:&nbsp;
              <span className={aiHits.length > 0 ? styles.danger : styles.count}>{aiHits.length}</span>
              &ensp;|&ensp;Synced:&nbsp;<span className={styles.count}>{syncLabel}</span>
              &ensp;|&ensp;<span className={styles.online}>BST (UTC+6)</span>
            </p>
          </div>
        </div>
        <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
          <button className={styles.backBtn} onClick={() => navigate("/dashboard")}>
            <FaArrowLeft />&nbsp;Dashboard
          </button>
          <div className={styles.liveBadge}>● LIVE</div>
        </div>
      </header>

      {/* Error */}
      {error && (
        <div className={styles.errorBanner}>
          <FaWifi />&nbsp;[ERR] Connection error: {error} — retrying in {POLL_INTERVAL / 1000}s…
        </div>
      )}

      {/* Tabs */}
      <div className={styles.tabs}>
        <button
          className={tab === "monitor" ? styles.tabActive : styles.tab}
          onClick={() => setTab("monitor")}
        >
          <FaSatellite />&nbsp;Live Teams
        </button>
        <button
          className={tab === "ai-hits" ? styles.tabActive : styles.tab}
          onClick={() => setTab("ai-hits")}
        >
          <FaDatabase />&nbsp;AI Hits Log
          {aiHits.length > 0 && <span className={styles.tabBadge}>{aiHits.length}</span>}
        </button>
      </div>

      {/* ── MONITOR TAB ── */}
      {tab === "monitor" && (
        <>
          {violations.length > 0 && (
            <section className={styles.violationPanel}>
              <div className={styles.violationPanelHeader}>
                <FaBan className={styles.violationPanelIcon} />
                <span>AI USAGE DETECTED — {violations.length} TEAM{violations.length > 1 ? "S" : ""} FLAGGED</span>
              </div>
              <div className={styles.violationList}>
                {violations.map((v, i) => (
                  <div key={v.ip || i} className={styles.violationEntry}>
                    <div className={styles.violationEntryHeader}>
                      <FaSkull className={styles.skullIcon} />
                      <span className={styles.violationTeamName}>{v.team_name}</span>
                      <code className={styles.violationIP}>{v.ip}</code>
                      <span className={styles.violationTime2}><FaClock />&nbsp;{v.detected_at} BST</span>
                    </div>
                    <div className={styles.violationMembers}>
                      <FaIdBadge className={styles.membersBadgeIcon} />
                      <div className={styles.memberChips}>
                        {v.members.map((m, mi) => (
                          <span key={mi} className={styles.memberChip}>{m}</span>
                        ))}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </section>
          )}

          <div className={styles.grid}>
            {loading ? (
              <div className={styles.loadingText}>&gt; Connecting to telemetry feed…</div>
            ) : teams.length === 0 ? (
              <div className={styles.empty}>
                <FaGlobe className={styles.emptyIcon} />
                <p>No teams found for this contest.</p>
              </div>
            ) : (
              teams.map((team, index) => {
                const isViolation = team.is_warning === true || team.is_warning === 1;
                return (
                  <div key={team.ip || index} className={isViolation ? styles.warningCard : styles.card}>
                    <div className={styles.cardHeader}>
                      {isViolation
                        ? <FaSkull style={{ color: "#ff4d4d" }} />
                        : <FaUserShield style={{ color: "#00ff41" }} />
                      }
                      <h2>{team.name}</h2>
                      {isViolation && <span className={styles.violationBadge}>VIOLATION</span>}
                    </div>
                    <div className={styles.memberList}>
                      <FaUsers className={styles.memberIcon} />
                      <p>{team.members}</p>
                    </div>
                    <div className={styles.stats}>
                      <div className={styles.statRow}>
                        <span>IP Address</span>
                        <code>{team.ip}</code>
                      </div>
                      <div className={styles.statRow}>
                        <span>AI Status</span>
                        <span className={isViolation ? styles.redText : styles.greenText}>
                          {isViolation
                            ? <><FaExclamationTriangle />&nbsp;AI DETECTED</>
                            : <><FaCheckCircle />&nbsp;CLEAN</>
                          }
                        </span>
                      </div>
                    </div>
                    <div className={styles.footer}>
                      {isViolation ? (
                        <div className={styles.violationTime}>
                          <FaClock />
                          <span>Detected at: <strong>{team.last_seen}</strong> BST</span>
                        </div>
                      ) : (
                        <span className={styles.cleanFooter}>✓ No violations logged</span>
                      )}
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </>
      )}

      {/* ── AI HITS TAB ── */}
      {tab === "ai-hits" && (
        <div className={styles.aiHitsSection}>
          {aiHits.length === 0 ? (
            <div className={styles.empty}>
              <FaDatabase className={styles.emptyIcon} />
              <p>No AI hits recorded yet for this contest.</p>
            </div>
          ) : (
            <table className={styles.hitsTable}>
              <thead>
                <tr>
                  <th>#</th>
                  <th>Time (BST)</th>
                  <th>IP</th>
                  <th>Team</th>
                  <th>Members</th>
                  <th>AI Domain</th>
                </tr>
              </thead>
              <tbody>
                {aiHits.map((hit, i) => (
                  <tr key={i} className={styles.hitRow}>
                    <td className={styles.hitIndex}>{i + 1}</td>
                    <td className={styles.hitTime}>
                      <FaClock style={{ marginRight: 5, color: "#ff4d4d" }} />{hit.hit_time}
                    </td>
                    <td><code className={styles.hitIP}>{hit.ip}</code></td>
                    <td className={styles.hitTeam}>
                      <FaFire style={{ marginRight: 5, color: "#ff4d4d" }} />{hit.team_name}
                    </td>
                    <td>
                      <div className={styles.memberChips}>
                        {hit.members.map((m, mi) => (
                          <span key={mi} className={styles.memberChip}>{m}</span>
                        ))}
                      </div>
                    </td>
                    <td><span className={styles.domainBadge}>{hit.domain}</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  );
};

export default MonitorContest;