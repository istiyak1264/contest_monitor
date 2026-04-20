import React from "react";
import { useNavigate } from "react-router-dom";
import { FaTerminal, FaShieldAlt, FaSatelliteDish, FaLock } from "react-icons/fa";
import styles from "./Home.module.css";

const Home = () => {
  const navigate = useNavigate();

  const handleProtectedNavigation = (path) => {
    const token = localStorage.getItem("token");
    
    if (token) {
      navigate(path);
    } else {
      navigate("/login"); 
    }
  };

  return (
    <div className={styles.container}>
      <div className={styles.heroSection}>
        <div className={styles.glitchWrapper}>
          <FaTerminal className={styles.heroIcon} />
          <h1 className={styles.title}>AI DETECTION IN COMPETETIVE PROGRAMMING</h1>
        </div>
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
            onClick={() => handleProtectedNavigation("/dashboard")} 
            className={styles.primaryBtn}
          >
            Open Dashboard
          </button>
          <button 
            onClick={() => handleProtectedNavigation("/host-contest")} 
            className={styles.secondaryBtn}
          >
            Host New Contest
          </button>
        </div>
      </div>
      <div className={styles.backgroundGrid}></div>
    </div>
  );
};

export default Home;