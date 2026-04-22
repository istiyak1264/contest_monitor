import React, { useState, useEffect } from "react";
import {
  FaLinkedin,
  FaFacebook,
  FaEnvelope,
  FaFileDownload,
  FaLock,
} from "react-icons/fa";
import styles from "./About.module.css";
import devPhoto from "../assets/dev_photo.png";

const About = () => {
  const [isRevealed, setIsRevealed] = useState(false);

  const socialLinks = {
    email:    "mailto:iamistiyakooo@gmail.com",
    linkedin: "https://www.linkedin.com/in/istiyak1264/",
    facebook: "https://www.facebook.com/istiyakahmed.cse15.pust",
    cv:       "/resume.pdf",
  };

  return (
    <div className={styles.container}>
      <div className={styles.scanline} />

      {/* Screenshot Protection Overlay */}
      {!isRevealed && (
        <div
          className={styles.overlay}
          onMouseDown={() => setIsRevealed(true)}
          onMouseUp={() => setIsRevealed(false)}
          onMouseEnter={() => setIsRevealed(true)}
          onMouseLeave={() => setIsRevealed(false)}
        >
          <FaLock size={40} className={styles.lockIcon} />
          <p className={styles.overlayLabel}>Hold to Reveal Info</p>
        </div>
      )}

      {/* Card */}
      <div className={`${styles.card} ${!isRevealed ? styles.blurred : ""}`}>

        {/* Avatar + Badge */}
        <div className={styles.profileHeader}>
          <div className={styles.imageContainer}>
            <img src={devPhoto} alt="Developer" className={styles.profileImg} />
          </div>
          <div className={styles.badge}>
            <span className={styles.badgeDot} />
            Open to Work
          </div>
        </div>

        {/* Bio */}
        <div className={styles.content}>
          <h1 className={styles.contentTitle}>About Me</h1>
          <p className={styles.contentText}>
            Hi, I am a dedicated developer building secure web applications.
            Check my <strong>resume</strong> for more information.
          </p>
        </div>

        {/* Divider */}
        <div className={styles.divider}>
          <span className={styles.dividerText}>contact &amp; links</span>
        </div>

        {/* Actions */}
        <div className={styles.footer}>
          <a href={socialLinks.cv} className={styles.downloadBtn} download>
            <FaFileDownload />
            &gt;&nbsp;Resume
          </a>

          <div className={styles.socials}>
            <a href={socialLinks.email} title="Email">
              <FaEnvelope />
            </a>
            <a href={socialLinks.linkedin} target="_blank" rel="noreferrer" title="LinkedIn">
              <FaLinkedin />
            </a>
            <a href={socialLinks.facebook} target="_blank" rel="noreferrer" title="Facebook">
              <FaFacebook />
            </a>
          </div>
        </div>

      </div>
    </div>
  );
};

export default About;