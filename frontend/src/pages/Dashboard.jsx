import React, { useState, useEffect, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import {
  FaTrophy, FaClock, FaTerminal, FaTrashAlt,
  FaExclamationTriangle, FaCheck, FaTimes, FaSatellite,
} from "react-icons/fa";
import { getUser } from "../api";
import styles from "./Dashboard.module.css";

const API = import.meta.env.VITE_API_URL;

function authHeaders(navigate) {
  const token = localStorage.getItem("token");
  if (!token) { navigate("/login"); return null; }
  return { Authorization: `Bearer ${token}` };
}

/** Format a millisecond diff into HH:MM:SS */
function fmtCountdown(diff) {
  const h = Math.floor(diff / 3600000);
  const m = Math.floor((diff % 3600000) / 60000);
  const s = Math.floor((diff % 60000) / 1000);
  return `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

const Dashboard = () => {
  const [contests, setContests]     = useState([]);
  const [loading, setLoading]       = useState(true);
  const [timeLeft, setTimeLeft]     = useState({});
  const [deletingId, setDeletingId] = useState(null);
  const navigate = useNavigate();
  const isAdmin  = getUser()?.role === "admin";

  const fetchContests = useCallback(async () => {
    const hdrs = authHeaders(navigate);
    if (!hdrs) return;
    try {
      const res = await fetch(`${API}/contests`, { headers: hdrs });
      if (res.status === 401) { navigate("/login"); return; }
      if (res.ok) {
        const data = await res.json();
        setContests(Array.isArray(data) ? data : []);
      }
    } catch (err) {
      console.error("Sync Error:", err);
    } finally {
      setLoading(false);
    }
  }, [navigate]);

  useEffect(() => { fetchContests(); }, [fetchContests]);

  // Countdown ticker
  useEffect(() => {
    if (contests.length === 0) return;
    const tick = () => {
      const now = Date.now();
      const next = {};
      contests.forEach(({ id, start_time }) => {
        const diff = new Date(start_time).getTime() - now;
        next[id] = diff <= 0 ? "LIVE" : fmtCountdown(diff);
      });
      setTimeLeft(next);
    };
    tick();
    const timer = setInterval(tick, 1000);
    return () => clearInterval(timer);
  }, [contests]);

  const confirmDelete = async (id) => {
    const hdrs = authHeaders(navigate);
    if (!hdrs) return;
    try {
      const res = await fetch(`${API}/contests/${id}`, { method: "DELETE", headers: hdrs });
      if (res.ok) {
        setContests((prev) => prev.filter((c) => c.id !== id));
        setDeletingId(null);
      }
    } catch (err) {
      console.error("Delete Error:", err);
    }
  };

  return (
    <div className={styles.container}>
      <div className={styles.scanline} />

      <header className={styles.header}>
        <div className={styles.titleArea}>
          <FaTerminal className={styles.mainIcon} />
          <div>
            <h1 className={styles.title}>Upcoming Contests<span className={styles.cursor}>_</span></h1>
            <p className={styles.subtext}>
              Active Deployments:&nbsp;<span className={styles.count}>{contests.length}</span>
              &ensp;|&ensp;System:&nbsp;<span className={styles.online}>BST (UTC+6)</span>
            </p>
          </div>
        </div>
      </header>

      <section className={styles.content}>
        {loading ? (
          <p className={styles.loadingText}>&gt; Synchronizing nodes...</p>
        ) : contests.length > 0 ? (
          <div className={styles.contestGrid}>
            {contests.map((contest) => (
              <div key={contest.id} className={styles.contestCard}>
                {deletingId !== contest.id ? (
                  <>
                    <div className={styles.cardTop}>
                      <div className={styles.contestHeader}>
                        <FaTrophy className={styles.trophySmall} />
                        <h3>{contest.name}</h3>
                      </div>
                      {isAdmin && (
                        <button
                          className={styles.deleteIconBtn}
                          onClick={() => setDeletingId(contest.id)}
                          aria-label="Delete contest"
                        >
                          <FaTrashAlt />
                        </button>
                      )}
                    </div>

                    <div className={styles.timerWrapper}>
                      <p className={styles.label}>// T-minus / status</p>
                      <div className={timeLeft[contest.id] === "LIVE" ? styles.liveBadge : styles.countdown}>
                        <FaClock />
                        <span>{timeLeft[contest.id] || "00:00:00"}</span>
                      </div>
                    </div>

                    <div className={styles.cardActions}>
                      <button
                        className={styles.manageBtn}
                        onClick={() => navigate(`/monitor-contest?id=${contest.id}`)}
                      >
                        <FaSatellite style={{ marginRight: 8 }} />
                        &gt;&nbsp;Open Telemetry
                      </button>
                    </div>
                  </>
                ) : (
                  <div className={styles.confirmOverlay}>
                    <FaExclamationTriangle className={styles.warnIcon} />
                    <p className={styles.confirmTitle}>[WARN] Terminate Operation?</p>
                    <p className={styles.deleteNote}>This removes the contest and all traffic/AI logs.</p>
                    <div className={styles.confirmActions}>
                      <button className={styles.cancelBtn} onClick={() => setDeletingId(null)}>
                        <FaTimes />&nbsp;Abort
                      </button>
                      <button className={styles.confirmBtn} onClick={() => confirmDelete(contest.id)}>
                        <FaCheck />&nbsp;Confirm
                      </button>
                    </div>
                  </div>
                )}
              </div>
            ))}
          </div>
        ) : (
          <div className={styles.emptyState}>
            <span className={styles.emptyIcon}>{'>'}</span>
            <p>No operations detected. Initialize a contest to begin.</p>
          </div>
        )}
      </section>
    </div>
  );
};

export default Dashboard;