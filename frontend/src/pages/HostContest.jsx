import React, { useState } from "react";
import {
  FaTrophy, FaCalendarAlt, FaHourglassHalf, FaFileCsv,
  FaUpload, FaCheckCircle, FaExclamationCircle,
} from "react-icons/fa";
import styles from "./HostContest.module.css";

const HostContest = () => {
  const [formData, setFormData] = useState({
    contestName: "",
    contestTime: "",
    duration: "120",
    csvFile: null,
  });
  const [status, setStatus] = useState({ loading: false, message: "", type: "" });

  const handleFileChange = (e) => {
    const file = e.target.files[0];
    if (file && file.name.endsWith(".csv")) {
      setFormData({ ...formData, csvFile: file });
      setStatus({ message: "", type: "" });
    } else {
      setStatus({ message: "Invalid file. Please upload a .csv file.", type: "error" });
    }
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!formData.csvFile)
      return setStatus({ message: "Please upload the team roster (CSV).", type: "error" });

    setStatus({ loading: true, message: "Deploying contest...", type: "" });

    const data = new FormData();
    data.append("contestName", formData.contestName);
    data.append("contestTime", formData.contestTime);
    data.append("duration", formData.duration);
    data.append("file", formData.csvFile);

    try {
      const res = await fetch("http://localhost:8080/host-contest", { method: "POST", body: data });
      if (res.ok) {
        setStatus({ loading: false, message: "Contest Deployed Successfully.", type: "success" });
        setFormData({ contestName: "", contestTime: "", duration: "120", csvFile: null });
      } else {
        setStatus({ loading: false, message: "Deployment failed. Check backend logs.", type: "error" });
      }
    } catch (err) {
      setStatus({ loading: false, message: "Cannot reach backend server.", type: "error" });
    }
  };

  return (
    <div className={styles.container}>
      <div className={styles.scanline} />

      <div className={styles.card}>
        <div className={styles.header}>
          <FaTrophy className={styles.icon} />
          <h1 className={styles.title}>Host New Contest<span className={styles.cursor}>_</span></h1>
          <p className={styles.subtitle}>Initialize real-time AI detection in Competetive Programming</p>
        </div>

        <form onSubmit={handleSubmit} className={styles.form}>

          <div className={styles.inputGroup}>
            <label className={styles.label}>Contest Title</label>
            <span className={styles.inputIcon}>$</span>
            <input
              className={styles.input}
              type="text"
              placeholder="e.g. Cyber Drill 2026"
              required
              value={formData.contestName}
              onChange={(e) => setFormData({ ...formData, contestName: e.target.value })}
            />
          </div>

          <div className={styles.row}>
            <div className={styles.inputGroup}>
              <label className={styles.label}><FaCalendarAlt />&nbsp;Start Time (BST)</label>
              <span className={styles.inputIcon}>@</span>
              <input
                className={styles.input}
                type="datetime-local"
                required
                value={formData.contestTime}
                onChange={(e) => setFormData({ ...formData, contestTime: e.target.value })}
              />
            </div>
            <div className={styles.inputGroup}>
              <label className={styles.label}><FaHourglassHalf />&nbsp;Duration (min)</label>
              <span className={styles.inputIcon}>#</span>
              <input
                className={styles.input}
                type="number"
                required
                value={formData.duration}
                onChange={(e) => setFormData({ ...formData, duration: e.target.value })}
              />
            </div>
          </div>

          <div className={styles.fileUpload}>
            <label htmlFor="csv" className={styles.fileLabel}>
              <FaFileCsv className={styles.csvIcon} />
              <span>{formData.csvFile ? formData.csvFile.name : "click to upload roster .csv"}</span>
              <small>format: team_name, ip, member1, member2, member3</small>
            </label>
            <input id="csv" type="file" accept=".csv" hidden onChange={handleFileChange} />
          </div>

          <button className={styles.submitBtn} type="submit" disabled={status.loading}>
            {status.loading
              ? <>&gt;&nbsp;Processing...</>
              : <><FaUpload />&nbsp;&gt;&nbsp;Launch Contest</>
            }
          </button>

          {status.message && (
            <div className={status.type === "success" ? styles.successMsg : styles.errorMsg}>
              {status.type === "success"
                ? <FaCheckCircle />
                : <FaExclamationCircle />
              }
              <span>{status.message}</span>
            </div>
          )}
        </form>
      </div>
    </div>
  );
};

export default HostContest;