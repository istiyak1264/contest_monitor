import React, { useState } from "react";
import { FaTrophy, FaCalendarAlt, FaHourglassHalf, FaFileCsv, FaUpload, FaCheckCircle, FaExclamationCircle } from "react-icons/fa";
import styles from "./HostContest.module.css";

const HostContest = () => {
  const [formData, setFormData] = useState({ 
    contestName: "", 
    contestTime: "", 
    duration: "120", 
    csvFile: null 
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
    if (!formData.csvFile) return setStatus({ message: "Please upload the team roster (CSV).", type: "error" });

    setStatus({ loading: true, message: "Deploying contest & initializing network sniffer...", type: "" });

    const data = new FormData();
    data.append("contestName", formData.contestName);
    data.append("contestTime", formData.contestTime);
    data.append("duration", formData.duration);
    data.append("file", formData.csvFile);

    try {
      const res = await fetch("http://localhost:8080/host-contest", { method: "POST", body: data });
      if (res.ok) {
        setStatus({ loading: false, message: "Contest successfully deployed. Sniffer active.", type: "success" });
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
      <div className={styles.card}>
        <div className={styles.header}>
          <FaTrophy className={styles.icon} />
          <h1>Host New Contest</h1>
          <p>Initialize real-time AI detection in Competetive Programming</p>
        </div>

        <form onSubmit={handleSubmit} className={styles.form}>
          <div className={styles.inputGroup}>
            <label>Contest Title</label>
            <input 
              type="text" 
              placeholder="e.g. Cyber Drill 2026"
              required 
              value={formData.contestName} 
              onChange={(e) => setFormData({...formData, contestName: e.target.value})} 
            />
          </div>

          <div className={styles.row}>
            <div className={styles.inputGroup}>
              <label><FaCalendarAlt /> Start Time (BST)</label>
              <input 
                type="datetime-local" 
                required 
                value={formData.contestTime} 
                onChange={(e) => setFormData({...formData, contestTime: e.target.value})} 
              />
            </div>

            <div className={styles.inputGroup}>
              <label><FaHourglassHalf /> Contest Duration (Minutes)</label>
              <input 
                type="number" 
                required 
                value={formData.duration} 
                onChange={(e) => setFormData({...formData, duration: e.target.value})} 
              />
            </div>
          </div>

          <div className={styles.fileUpload}>
            <label htmlFor="csv" className={styles.fileLabel}>
              <FaFileCsv size={32} />
              <span>{formData.csvFile ? formData.csvFile.name : "Click to upload the csv file"}</span>
              <small>Format: team_name, ip, member1, member2, member3</small>
            </label>
            <input id="csv" type="file" accept=".csv" hidden onChange={handleFileChange} />
          </div>

          <button type="submit" className={styles.submitBtn} disabled={status.loading}>
            {status.loading ? "Processing..." : <><FaUpload /> Launch Contest</>}
          </button>

          {status.message && (
            <div className={status.type === "success" ? styles.successMsg : styles.errorMsg}>
              {status.type === "success" ? <FaCheckCircle /> : <FaExclamationCircle />}
              <p>{status.message}</p>
            </div>
          )}
        </form>
      </div>
    </div>
  );
};

export default HostContest;