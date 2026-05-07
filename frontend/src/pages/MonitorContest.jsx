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

function getAuthHeader(navigate) {
  const token = localStorage.getItem("token");
  if (!token) { navigate("/login"); return null; }
  return { Authorization: `Bearer ${token}` };
}

async function apiFetch(url, headers) {
  try {
    const res = await fetch(url, { cache: "no-store", headers });
    if (res.status === 401) return { unauthorized: true };
    if (!res.ok) return null;
    return await res.json();
  } catch {
    return null;
  }
}

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

  // ── Contest picker (no id in URL) ─────────────────────────────────────────
  useEffect(() => {
    if (contestId) return;
    const hdrs = getAuthHeader(navigate);
    if (!hdrs) return;
    setPickerLoading(true);
    apiFetch(`${API}/contests`, hdrs)
      .then((data) => {
        if (data?.unauthorized) { navigate("/login"); return; }
        setContests(Array.isArray(data) ? data : []);
      })
      .finally(() => setPickerLoading(false));
  }, [contestId, navigate]);

  // ── Polling ───────────────────────────────────────────────────────────────
  const poll = useCallback(async () => {
    if (!contestId) return;
    const hdrs = getAuthHeader(navigate);
    if (!hdrs) return;

    // Fire all three requests in parallel
    const [teamsData, violData, hitsData] = await Promise.all([
      apiFetch(`${API}/contests/${contestId}/monitor`, hdrs),
      apiFetch(`${API}/contests/${contestId}/violations`, hdrs),
      apiFetch(`${API}/contests/${contestId}/ai-hits`, hdrs),
    ]);

    if (teamsData?.unauthorized || violData?.unauthorized || hitsData?.unauthorized) {
      navigate("/login");
      return;
    }

    if (teamsData === null) {
      setError("Connection error — retrying...");
    } else {
      setTeams(Array.isArray(teamsData) ? teamsData : []);
      setError(null);
      setLastSync(new Date());
    }

    if (Array.isArray(violData)) {
      if (violData.length > prevViolations.current) playAlert();
      prevViolations.current = violData.length;
      setViolations(violData);
    }

    if (Array.isArray(hitsData)) {
      if (hitsData.length > prevAiHits.current) playAlert();
      prevAiHits.current = hitsData.length;
      setAiHits(hitsData);
    }

    setLoading(false);
  }, [contestId, navigate, playAlert]);

  useEffect(() => {
    if (!contestId) return;
    poll();
    intervalRef.current = setInterval(poll, POLL_INTERVAL);
    return () => clearInterval(intervalRef.current);
  }, [contestId, poll]);

  const syncLabel = lastSync
    ? lastSync.toLocaleTimeString("en-BD", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })
    : "---";

  const violationCount = teams.filter((t) => t.is_warning === true || t.is_warning === 1).length;

  /* ── Contest picker ── */
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
                <span className={styles.pickerSub}>Click to monitor</span>
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
          <FaWifi />&nbsp;[ERR] {error}
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
                <span>AI USAGE DETECTED — {violations.length} TEAM{violations.length !== 1 ? "S" : ""} FLAGGED</span>
              </div>
              <div className={styles.violationList}>
                {violations.map((v, i) => (
                  <div key={v.ip || i} className={styles.violationEntry}>
                    <div className={styles.violationEntryHeader}>
                      <FaSkull className={styles.skullIcon} />
                      <span className={styles.violationTeamName}>{v.team_name}</span>
                      <code className={styles.violationIP}>{v.ip}</code>
                      {/* Fixed: was styles.violationTime2, now styles.violationTimestamp */}
                      <span className={styles.violationTimestamp}>
                        <FaClock />&nbsp;{v.detected_at} BST
                      </span>
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
                          <span>Detected: <strong>{team.last_seen} BST</strong></span>
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