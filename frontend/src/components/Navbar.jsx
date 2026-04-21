import React, { useState, useEffect, useRef } from "react";
import { FaHome, FaBars, FaTimes } from "react-icons/fa";
import { Link, useNavigate, useLocation } from "react-router-dom";
import styles from "./Navbar.module.css";

const Navbar = () => {
  const [menuOpen, setMenuOpen] = useState(false);
  const [isLoggedIn, setIsLoggedIn] = useState(!!localStorage.getItem("token"));
  const [scrolled, setScrolled] = useState(false);
  const [glitching, setGlitching] = useState(false);
  const glitchTimer = useRef(null);
  const navigate = useNavigate();
  const location = useLocation();

  /* ── Auth check ── */
  useEffect(() => {
    const checkAuth = () => setIsLoggedIn(!!localStorage.getItem("token"));
    checkAuth();
    window.addEventListener("storage", checkAuth);
    return () => window.removeEventListener("storage", checkAuth);
  }, [location]);

  /* ── Scroll-aware compact mode ── */
  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 20);
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  /* ── Periodic logo glitch ── */
  useEffect(() => {
    const scheduleGlitch = () => {
      const delay = 4000 + Math.random() * 6000;
      glitchTimer.current = setTimeout(() => {
        setGlitching(true);
        setTimeout(() => {
          setGlitching(false);
          scheduleGlitch();
        }, 600);
      }, delay);
    };
    scheduleGlitch();
    return () => clearTimeout(glitchTimer.current);
  }, []);

  /* ── Lock body scroll when mobile menu open ── */
  useEffect(() => {
    document.body.style.overflow = menuOpen ? "hidden" : "";
    return () => { document.body.style.overflow = ""; };
  }, [menuOpen]);

  const toggleMenu = () => setMenuOpen((v) => !v);
  const closeMenu = () => setMenuOpen(false);

  const handleLogout = () => {
    localStorage.removeItem("token");
    localStorage.removeItem("user");
    setIsLoggedIn(false);
    closeMenu();
    navigate("/login");
  };

  return (
    <nav
      className={`${styles.navbar} ${scrolled ? styles.scrolled : ""}`}
      role="navigation"
      aria-label="Main navigation"
    >
      {/* Animated scanning top border */}
      <span className={styles.topBorder} aria-hidden="true" />

      {/* ── LEFT: Logo ── */}
      <div className={styles.leftSection}>
        <Link
          to="/"
          className={`${styles.navLogo} ${glitching ? styles.glitch : ""}`}
          onClick={closeMenu}
          data-text="Home"
          aria-label="Go to home"
        >
          <span className={styles.logoIcon}><FaHome size={20} /></span>
          <span className={styles.logoText}>Home</span>
          <span className={styles.logoCaret} aria-hidden="true">_</span>
        </Link>

        {/* Live status indicator */}
        <span className={styles.statusDot} title="System online" aria-hidden="true">
          <span className={styles.statusPulse} />
        </span>
      </div>

      {/* ── CENTER: Nav links ── */}
      <div
        className={`${styles.navLinks} ${menuOpen ? styles.active : ""}`}
        role="menu"
      >
        {!isLoggedIn ? (
          <>
            <NavLink to="/login" onClick={closeMenu} index={0}>Login</NavLink>
            <NavLink to="/about" onClick={closeMenu} index={1}>About</NavLink>
            <NavLink to="/register" onClick={closeMenu} index={2}>Get Started</NavLink>
          </>
        ) : (
          <>
            <NavLink to="/dashboard" onClick={closeMenu} index={0}>Dashboard</NavLink>
            <NavLink to="/monitor-contest" onClick={closeMenu} index={1}>Monitor Contest</NavLink>
            <NavLink to="/host-contest" onClick={closeMenu} index={2}>Host Contest</NavLink>
          </>
        )}
      </div>

      {/* ── RIGHT: Action + Hamburger ── */}
      <div className={styles.rightSection}>
        {isLoggedIn ? (
          <button onClick={handleLogout} className={styles.logoutBtn} aria-label="Logout">
            <span className={styles.btnBracket}>[</span>
            <span>Logout</span>
            <span className={styles.btnBracket}>]</span>
          </button>
        ) : (
          <div style={{ width: "80px" }} className={styles.desktopOnly} />
        )}

        <button
          className={styles.hamburger}
          onClick={toggleMenu}
          aria-label={menuOpen ? "Close menu" : "Open menu"}
          aria-expanded={menuOpen}
        >
          {menuOpen ? <FaTimes size={20} /> : <FaBars size={20} />}
        </button>
      </div>

      {/* Mobile backdrop */}
      <div
        className={`${styles.backdrop} ${menuOpen ? styles.backdropVisible : ""}`}
        onClick={closeMenu}
        aria-hidden="true"
      />
    </nav>
  );
};

/* ── NavLink sub-component with active detection ── */
const NavLink = ({ to, onClick, index, children }) => {
  const location = useLocation();
  const isActive = location.pathname === to;

  return (
    <Link
      to={to}
      onClick={onClick}
      role="menuitem"
      className={`${styles.navItem} ${isActive ? styles.navItemActive : ""}`}
      style={{ "--i": index }}
    >
      <span className={styles.navItemPrefix} aria-hidden="true">
        {isActive ? "▶" : ""}
      </span>
      <span>{children}</span>
      {isActive && <span className={styles.activeBar} aria-hidden="true" />}
    </Link>
  );
};

export default Navbar;
