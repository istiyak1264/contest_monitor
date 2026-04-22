import React, { useState, useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { FaTerminal, FaShieldAlt, FaSatelliteDish, FaLock } from "react-icons/fa";
import styles from "./Home.module.css";

const TYPED_TEXT = "// ai detection in competitive programming";

const Home = () => {
  const navigate = useNavigate();
  const [typed, setTyped] = useState("");

  useEffect(() => {
    let i = 0;
    const interval = setInterval(() => {
      setTyped(TYPED_TEXT.slice(0, i + 1));
      i++;
      if (i >= TYPED_TEXT.length) clearInterval(interval);
    }, 45);
    return () => clearInterval(interval);
  }, []);

  const handleProtectedNavigation = (path) => {
    const token = localStorage.getItem("token");
    navigate(token ? path : "/login");
  };

  return (
    <div className={styles.container}>
      <div className={styles.scanline} />

      <div className={styles.heroSection}>
        <FaTerminal className={styles.heroIcon} />

        <h1 className={styles.title}>
          AI Detection<span className={styles.cursor}>_</span>
        </h1>

        <p className={styles.typewriter}>{typed}<span className={styles.caret}>|</span></p>

        <p className={styles.subtitle}>
          Monitor nodes, analyze traffic, and manage deployments in real-time.
        </p>

        <div className={styles.features}>
          <div className={styles.featureItem}>
            <FaShieldAlt className={styles.fIcon} />
            <span>Secure Nodes</span>
          </div>
          <div className={styles.featureItem}>
            <FaSatelliteDish className={styles.fIcon} />
            <span>Real-time Sync</span>
          </div>
          <div className={styles.featureItem}>
            <FaLock className={styles.fIcon} />
            <span>Encrypted</span>
          </div>
        </div>

        <div className={styles.buttonGroup}>
          <button
            className={styles.primaryBtn}
            onClick={() => handleProtectedNavigation("/dashboard")}
          >
            &gt;&nbsp;Open Dashboard
          </button>
          <button
            className={styles.secondaryBtn}
            onClick={() => handleProtectedNavigation("/host-contest")}
          >
            &gt;&nbsp;Host New Contest
          </button>
        </div>
      </div>
    </div>
  );
};

export default Home;