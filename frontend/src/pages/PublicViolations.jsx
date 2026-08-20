import React, { useCallback, useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import {
  FaArrowLeft,
  FaBan,
  FaClock,
  FaDatabase,
  FaExclamationTriangle,
  FaGlobe,
  FaSatellite,
} from "react-icons/fa";
import { apiGet, isLoggedIn } from "../api";
import styles from "./MonitorContest.module.css";

const POLL_INTERVAL = 5000;

async function fetchJson(path) {
  try {
    const response = await apiGet(path);
    if (response.status === 401) return { unauthorized: true };
    if (!response.ok) return null;
    return await response.json();
  } catch {
    return null;
  }
}

const PublicViolations = () => {
  const [contests, setContests] = useState([]);
  const [violations, setViolations] = useState([]);
  const [loading, setLoading] = useState(true);
  const [pickerLoading, setPickerLoading] = useState(true);
  const [error, setError] = useState(null);
  const [lastSync, setLastSync] = useState(null);
  const location = useLocation();
  const navigate = useNavigate();
  const contestId = new URLSearchParams(location.search).get("id");

  useEffect(() => {
    if (contestId) return undefined;
    if (!isLoggedIn()) {
      navigate("/login");
      return undefined;
    }
    fetchJson("/contests")
      .then((data) => {
        if (data?.unauthorized) {
          navigate("/login");
          return;
        }
        setContests(Array.isArray(data) ? data : []);
      })
      .finally(() => setPickerLoading(false));
    return undefined;
  }, [contestId, navigate]);

  const poll = useCallback(async () => {
    if (!contestId) return;
    if (!isLoggedIn()) {
      navigate("/login");
      return;
    }
    const data = await fetchJson(`/contests/${contestId}/public-violations`);
    if (data?.unauthorized) {
      navigate("/login");
      return;
    }
    if (data === null) {
      setError("Connection error — retrying...");
      setLoading(false);
      return;
    }
    setViolations(Array.isArray(data) ? data : []);
    setError(null);
    setLastSync(new Date());
    setLoading(false);
  }, [contestId, navigate]);

  useEffect(() => {
    if (!contestId) return undefined;
    const firstPoll = window.setTimeout(() => { void poll(); }, 0);
    const interval = window.setInterval(() => { void poll(); }, POLL_INTERVAL);
    return () => {
      window.clearTimeout(firstPoll);
      window.clearInterval(interval);
    };
  }, [contestId, poll]);

  const syncLabel = lastSync
    ? lastSync.toLocaleTimeString("en-BD", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })
    : "---";

  if (!contestId) {
    return (
      <div className={styles.container}>
        <div className={styles.scanline} />
        <header className={styles.header}>
          <div className={styles.headerInfo}>
            <FaSatellite className={styles.mainIcon} />
            <div>
              <h1 className={styles.title}>Shared Violations<span className={styles.cursor}>_</span></h1>
              <p className={styles.subtext}>// read-only feed for authenticated LAN users</p>
            </div>
          </div>
        </header>
        {pickerLoading ? (
          <div className={styles.loadingText}>&gt; Loading contests...</div>
        ) : contests.length === 0 ? (
          <div className={styles.empty}>
            <FaGlobe className={styles.emptyIcon} />
            <p>No contests found.</p>
          </div>
        ) : (
          <div className={styles.pickerGrid}>
            {contests.map((contest) => (
              <button
                key={contest.id}
                className={styles.pickerCard}
                onClick={() => navigate(`/violations?id=${contest.id}`)}
              >
                <FaDatabase className={styles.pickerIcon} />
                <span className={styles.pickerName}>{contest.name}</span>
                <span className={styles.pickerSub}>Open shared feed</span>
              </button>
            ))}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className={styles.container}>
      <div className={styles.scanline} />
      <header className={styles.header}>
        <div className={styles.headerInfo}>
          <FaSatellite className={styles.mainIcon} />
          <div>
            <h1 className={styles.title}>Shared Violations<span className={styles.cursor}>_</span></h1>
            <p className={styles.subtext}>
              Read-only LAN feed&nbsp;|&nbsp;Last sync: <span className={styles.count}>{syncLabel} BST</span>
              &nbsp;|&nbsp;Flagged teams: <span className={violations.length ? styles.danger : styles.count}>{violations.length}</span>
            </p>
          </div>
        </div>
        <button className={styles.backBtn} onClick={() => navigate("/violations")}>
          <FaArrowLeft />&nbsp;Contests
        </button>
      </header>

      {error && (
        <div className={styles.errorBanner}>
          <FaDatabase />&nbsp;[ERR] {error}
        </div>
      )}

      <div className={styles.tabs}>
        <div className={styles.tabActive}>
          <FaBan />&nbsp;Current Violations
        </div>
      </div>

      <div className={styles.violationPanel}>
        <div className={styles.violationPanelHeader}>
          <FaExclamationTriangle className={styles.violationPanelIcon} />
          <span>CONSENTED READ-ONLY FEED</span>
        </div>
        <p className={styles.subtext}>
          This shared view intentionally excludes participant IP addresses, member lists, and administrative controls.
        </p>
        {loading ? (
          <div className={styles.loadingText}>&gt; Synchronizing violations...</div>
        ) : violations.length === 0 ? (
          <div className={styles.empty}>
            <FaGlobe className={styles.emptyIcon} />
            <p>No violations recorded for this contest.</p>
          </div>
        ) : (
          <div className={styles.violationList}>
            {violations.map((violation) => (
              <div key={`${violation.team_name}-${violation.detected_at}`} className={styles.violationEntry}>
                <div className={styles.violationEntryHeader}>
                  <FaExclamationTriangle className={styles.skullIcon} />
                  <span className={styles.violationTeamName}>{violation.team_name}</span>
                  <span className={styles.violationTimestamp}>
                    <FaClock />&nbsp;{violation.detected_at} BST
                  </span>
                </div>
                <div className={styles.violationMembers}>
                  <span className={styles.domainBadge}>{violation.domain || "service signal recorded"}</span>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

export default PublicViolations;
