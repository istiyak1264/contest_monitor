import React, { useState } from "react";
import { 
  FaLinkedin, 
  FaFacebook, 
  FaEnvelope, 
  FaFileDownload, 
  FaBriefcase, 
  FaLock 
} from "react-icons/fa";
import styles from "./About.module.css";
import devPhoto from "../assets/dev_photo.png"; 

const About = () => {
  const [isRevealed, setIsRevealed] = useState(false);

  const socialLinks = {
    email: "mailto:iamistiyakooo@gmail.com",
    linkedin: "https://www.linkedin.com/in/istiyak1264/",
    facebook: "https://www.facebook.com/istiyakahmed.cse15.pust",
    cv: "/resume.pdf"
  };

  return (
    <div className={styles.container}>
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
          <p>Hold to Reveal Info</p>
        </div>
      )}

      {/* Optimized Content Card */}
      <div className={`${styles.card} ${!isRevealed ? styles.blurred : ""}`}>
        <div className={styles.profileHeader}>
          <div className={styles.imageContainer}>
            <img src={devPhoto} alt="Developer" className={styles.profileImg} />
          </div>
          <div className={styles.badge}>
            <FaBriefcase size={12} />
            <span>Open to Work</span>
          </div>
        </div>

        <div className={styles.content}>
          <h1>About Me</h1>
          <p>
            I am a dedicated developer building secure web applications 
            like the <strong>AI Contest Monitor</strong>, bridging the gap 
            between real-time data and user experience.
          </p>
        </div>

        <div className={styles.footer}>
          <a href={socialLinks.cv} className={styles.downloadBtn} download>
            <FaFileDownload /> Resume
          </a>

          <div className={styles.socials}>
            <a href={socialLinks.email} title="Email"><FaEnvelope /></a>
            <a href={socialLinks.linkedin} target="_blank" rel="noreferrer" title="LinkedIn"><FaLinkedin /></a>
            <a href={socialLinks.facebook} target="_blank" rel="noreferrer" title="Facebook"><FaFacebook /></a>
          </div>
        </div>
      </div>
    </div>
  );
};

export default About;