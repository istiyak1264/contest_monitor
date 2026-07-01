import React, { useState, useEffect } from "react";
import { useNavigate } from "react-router-dom";
import {
  FaTrophy, FaCalendarAlt, FaHourglassHalf, FaFileCsv,
  FaUpload, FaCheckCircle, FaExclamationCircle, FaClock,
} from "react-icons/fa";
import { getUser } from "../api";
import styles from "./HostContest.module.css";

const API         = import.meta.env.VITE_API_URL;
const MAX_DURATION = 300; // minutes

// Pad number to 2 digits
const pad = (n) => String(n).padStart(2, "0");

const HostContest = () => {
  const navigate = useNavigate();
  const today = new Date().toISOString().split("T")[0];

  useEffect(() => {
    const isAdminVerified = localStorage.getItem("adminVerified") === "true";
    if (getUser()?.role !== "admin" || !isAdminVerified) {
      navigate("/dashboard");
    }
  }, [navigate]);

  const [contestName, setContestName] = useState("");
  const [date, setDate]               = useState("");
  const [hour, setHour]               = useState("12");
  const [minute, setMinute]           = useState("00");
  const [ampm, setAmpm]               = useState("AM");
  const [duration, setDuration]       = useState("120");
  const [csvFile, setCsvFile]         = useState(null);
  const [status, setStatus]           = useState({ loading: false, message: "", type: "" });

  const hours   = Array.from({ length: 12 }, (_, i) => pad(i + 1));
  const minutes = Array.from({ length: 60 }, (_, i) => pad(i));

  const buildContestTime = () => {
    let h = parseInt(hour, 10);
    if (ampm === "AM" && h === 12) h = 0;
    if (ampm === "PM" && h !== 12) h += 12;
    return `${date}T${pad(h)}:${minute}`;
  };

  const handleFileChange = (e) => {
    const file = e.target.files[0];
    if (!file) return;
    if (!file.name.toLowerCase().endsWith(".csv")) {
      setStatus({ loading: false, message: "Invalid file — upload a .csv", type: "error" });
      return;
    }
    setCsvFile(file);
    setStatus({ loading: false, message: "", type: "" });
  };

  const handleDurationChange = (e) => {
    const val = e.target.value;
    if (val === "" || (Number(val) >= 1 && Number(val) <= MAX_DURATION)) {
      setDuration(val);
    }
  };

  const handleDurationBlur = () => {
    const val = parseInt(duration, 10);
    if (isNaN(val) || val < 1) setDuration("1");
    else if (val > MAX_DURATION) setDuration(String(MAX_DURATION));
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!date)    return setStatus({ loading: false, message: "Select a contest date.", type: "error" });
    if (!csvFile) return setStatus({ loading: false, message: "Upload the team roster (.csv).", type: "error" });

    const dur = parseInt(duration, 10);
    if (isNaN(dur) || dur < 1 || dur > MAX_DURATION) {
      return setStatus({ loading: false, message: `Duration must be 1–${MAX_DURATION} min.`, type: "error" });
    }

    const token = localStorage.getItem("token");
    if (!token) return setStatus({ loading: false, message: "Not logged in. Please login first.", type: "error" });

    setStatus({ loading: true, message: "Deploying contest...", type: "" });

    const data = new FormData();
    data.append("contestName", contestName.trim());
    data.append("contestTime", buildContestTime());
    data.append("duration", String(dur));
    data.append("file", csvFile);

    try {
      const res  = await fetch(`${API}/host-contest`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
        body: data,
      });
      const json = await res.json().catch(() => null);

      if (res.ok) {
        setStatus({ loading: false, message: "Contest deployed successfully!", type: "success" });
        setContestName(""); setDate(""); setHour("12"); setMinute("00");
        setAmpm("AM"); setDuration("120"); setCsvFile(null);
        const fi = document.getElementById("csv-upload");
        if (fi) fi.value = "";
      } else {
        setStatus({ loading: false, message: json?.error || `Server error ${res.status}`, type: "error" });
      }
    } catch {
      setStatus({ loading: false, message: "Cannot reach backend server.", type: "error" });
    }
  };

  const durVal = parseInt(duration, 10) || 0;

  return (
    <div className={styles.container}>
      <div className={styles.scanline} />

      <div className={styles.card}>
        {/* Header */}
        <div className={styles.header}>
          <FaTrophy className={styles.icon} />
          <div>
            <h1 className={styles.title}>Host Contest<span className={styles.cursor}>_</span></h1>
            <p className={styles.subtitle}>// initialize real-time AI detection</p>
          </div>
        </div>

        <form onSubmit={handleSubmit} className={styles.form}>

          {/* Contest Name */}
          <div className={styles.field}>
            <label className={styles.label}>Contest Title</label>
            <div className={styles.inputWrap}>
              <span className={styles.prefix}>$</span>
              <input
                className={styles.input}
                type="text"
                placeholder="e.g. PUST Cyber Drill 2026"
                required
                value={contestName}
                onChange={(e) => setContestName(e.target.value)}
              />
            </div>
          </div>

          {/* Date + Time row */}
          <div className={styles.row2}>
            <div className={styles.field}>
              <label className={styles.label}><FaCalendarAlt /> Date</label>
              <input
                className={styles.input}
                type="date"
                required
                value={date}
                min={today}
                onChange={(e) => setDate(e.target.value)}
              />
            </div>

            <div className={styles.field}>
              <label className={styles.label}><FaClock /> Start (BST)</label>
              <div className={styles.timeRow}>
                <select className={styles.sel} value={hour} onChange={(e) => setHour(e.target.value)}>
                  {hours.map((h) => <option key={h}>{h}</option>)}
                </select>
                <span className={styles.colon}>:</span>
                <select className={styles.sel} value={minute} onChange={(e) => setMinute(e.target.value)}>
                  {minutes.map((m) => <option key={m}>{m}</option>)}
                </select>
                <div className={styles.ampm}>
                  {["AM", "PM"].map((p) => (
                    <button key={p} type="button"
                      className={ampm === p ? styles.ampmOn : styles.ampmOff}
                      onClick={() => setAmpm(p)}>{p}
                    </button>
                  ))}
                </div>
              </div>
            </div>
          </div>

          {/* Duration */}
          <div className={styles.field}>
            <label className={styles.label}>
              <FaHourglassHalf /> Duration (min)
              <span className={styles.hint}>{Math.floor(durVal / 60)}h {durVal % 60}m · max {MAX_DURATION}</span>
            </label>
            <input
              className={styles.input}
              type="number"
              required
              min={1}
              max={MAX_DURATION}
              value={duration}
              onChange={handleDurationChange}
              onBlur={handleDurationBlur}
            />
            <div className={styles.bar}>
              <div className={styles.barFill}
                style={{ width: `${Math.min((durVal / MAX_DURATION) * 100, 100)}%` }} />
            </div>
          </div>

          {/* CSV Upload */}
          <div className={styles.upload}>
            <label htmlFor="csv-upload" className={styles.uploadLabel}>
              <FaFileCsv className={styles.csvIcon} />
              <span>{csvFile ? csvFile.name : "click to upload roster .csv"}</span>
              <small>team_name, ip, member1, member2, member3</small>
            </label>
            <input id="csv-upload" type="file" accept=".csv" hidden onChange={handleFileChange} />
          </div>

          {/* Submit */}
          <button className={styles.btn} type="submit" disabled={status.loading}>
            {status.loading
              ? "> Deploying..."
              : <><FaUpload /> &gt; Launch Contest</>
            }
          </button>

          {/* Status */}
          {status.message && (
            <div className={status.type === "success" ? styles.ok : styles.err}>
              {status.type === "success" ? <FaCheckCircle /> : <FaExclamationCircle />}
              <span>{status.message}</span>
            </div>
          )}
        </form>
      </div>
    </div>
  );
};

export default HostContest;