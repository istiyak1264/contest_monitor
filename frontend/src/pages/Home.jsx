import React, { useState, useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { FaTerminal, FaShieldAlt, FaSatelliteDish, FaLock } from "react-icons/fa";
import { getUser } from "../api";
import styles from "./Home.module.css";

const TYPED_TEXT = "// ai detection in competitive programming";

const Home = () => {
  const navigate = useNavigate();
  const [typed, setTyped] = useState("");
  const [copied, setCopied] = useState(false);
  const isAdmin = getUser()?.role === "admin";
  const currentPortal = typeof window === "undefined" ? "" : window.location.origin;

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

  const copyPortalAddress = async () => {
    try {
      await navigator.clipboard.writeText(currentPortal);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1800);
    } catch {
      setCopied(false);
    }
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

        <div className={styles.networkPanel}>
          <div>
            <span className={styles.networkLabel}>AUTHORIZED LAN PORTAL</span>
            <p>Share the contest host&apos;s LAN URL with participants on the same Wi-Fi. The host must keep this service running, and the router must allow peer-to-peer access.</p>
            <code>{currentPortal}</code>
          </div>
          <button type="button" className={styles.copyBtn} onClick={copyPortalAddress}>
            {copied ? "Copied" : "Copy URL"}
          </button>
        </div>

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

        <p className={styles.disclaimer}>Detection uses consent-based DNS and TLS host metadata only. It does not decrypt traffic, capture packet payloads, or establish conclusive proof of AI use.</p>

        <div className={styles.buttonGroup}>
          <button
            className={styles.primaryBtn}
            onClick={() => handleProtectedNavigation("/dashboard")}
          >
            &gt;&nbsp;Open Dashboard
          </button>
          {isAdmin && (
            <button
              className={styles.secondaryBtn}
              onClick={() => handleProtectedNavigation("/host-contest")}
            >
              &gt;&nbsp;Host New Contest
            </button>
          )}
        </div>
      </div>
    </div>
  );
};

export default Home;