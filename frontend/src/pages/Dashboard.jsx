import React, { useState, useEffect, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import {
  FaTrophy,
  FaClock,
  FaTerminal,
  FaTrashAlt,
  FaExclamationTriangle,
  FaCheck,
  FaTimes,
  FaSatellite,
} from "react-icons/fa";
import styles from "./Dashboard.module.css";

const Dashboard = () => {
  const [contests, setContests]     = useState([]);
  const [loading, setLoading]       = useState(true);
  const [timeLeft, setTimeLeft]     = useState({});
  const [deletingId, setDeletingId] = useState(null);

  const navigate = useNavigate();

  const fetchContests = useCallback(async () => {
    try {
      const response = await fetch("http://localhost:8080/contests");
      if (response.ok) {
        const data = await response.json();
        setContests(data);
      }
    } catch (err) {
      console.error("Sync Error:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchContests(); }, [fetchContests]);

  const confirmDelete = async (id) => {
    try {
      const response = await fetch(`http://localhost:8080/contests/${id}`, { method: "DELETE" });
      if (response.ok) {
        setContests((prev) => prev.filter((c) => c.id !== id));
        setDeletingId(null);
      }
    } catch (err) {
      console.error("Delete Error:", err);
    }
  };

  useEffect(() => {
    if (contests.length === 0) return;
    const timer = setInterval(() => {
      const now = new Date();
      const newTimeLeft = {};
      contests.forEach((contest) => {
        const diff = new Date(contest.start_time) - now;
        if (diff <= 0) {
          newTimeLeft[contest.id] = "LIVE";
        } else {
          const h = Math.floor(diff / 3600000);
          const m = Math.floor((diff % 3600000) / 60000);
          const s = Math.floor((diff % 60000) / 1000);
          newTimeLeft[contest.id] =
            `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
        }
      });
      setTimeLeft(newTimeLeft);
    }, 1000);
    return () => clearInterval(timer);
  }, [contests]);

  return (
    <div className={styles.container}>
      <header className={styles.header}>
        <div className={styles.titleArea}>
          <FaTerminal className={styles.mainIcon} />
          <div>
            <h1>Upcoming Contests</h1>
            <p>
              Active Deployments: <span className={styles.count}>{contests.length}</span>
              {" "}| System: <span className={styles.online}>BST (UTC+6)</span>
            </p>
          </div>
        </div>
      </header>

      <section className={styles.content}>
        {loading ? (
          <p className={styles.loadingText}>Synchronizing Nodes...</p>
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
                      <button className={styles.deleteIconBtn} onClick={() => setDeletingId(contest.id)}>
                        <FaTrashAlt />
                      </button>
                    </div>

                    <div className={styles.timerWrapper}>
                      <p className={styles.label}>T-Minus / Status</p>
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
                        Open Telemetry
                      </button>
                    </div>
                  </>
                ) : (
                  <div className={styles.confirmOverlay}>
                    <FaExclamationTriangle className={styles.warnIcon} />
                    <p>Terminate Operation?</p>
                    <p className={styles.deleteNote}>This removes the contest and all traffic/AI logs.</p>
                    <div className={styles.confirmActions}>
                      <button className={styles.cancelBtn} onClick={() => setDeletingId(null)}>
                        <FaTimes /> No
                      </button>
                      <button className={styles.confirmBtn} onClick={() => confirmDelete(contest.id)}>
                        <FaCheck /> Yes
                      </button>
                    </div>
                  </div>
                )}
              </div>
            ))}
          </div>
        ) : (
          <div className={styles.emptyState}>
            <p>No operations detected. Initialize a contest to begin.</p>
          </div>
        )}
      </section>
    </div>
  );
};

export default Dashboard;
