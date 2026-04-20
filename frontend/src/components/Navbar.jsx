import React, { useState, useEffect } from "react";
import { FaHome, FaBars, FaTimes } from "react-icons/fa";
import { Link, useNavigate, useLocation } from "react-router-dom";
import styles from "./Navbar.module.css";

const Navbar = () => {
  const [menuOpen, setMenuOpen] = useState(false);
  const [isLoggedIn, setIsLoggedIn] = useState(!!localStorage.getItem("token"));
  const navigate = useNavigate();
  const location = useLocation();

  useEffect(() => {
    const checkAuth = () => {
      const token = localStorage.getItem("token");
      setIsLoggedIn(!!token);
    };

    checkAuth();
    window.addEventListener("storage", checkAuth);
    return () => window.removeEventListener("storage", checkAuth);
  }, [location]);

  const toggleMenu = () => setMenuOpen(!menuOpen);

  const handleLogout = () => {
    localStorage.removeItem("token");
    localStorage.removeItem("user");
    setIsLoggedIn(false);
    setMenuOpen(false);
    navigate("/login");
  };

  return (
    <nav className={styles.navbar}>
      <div className={styles.leftSection}>
        <Link to="/" className={styles.navLogo} onClick={() => setMenuOpen(false)}>
          <FaHome size={28} />
          <span>Home</span>
        </Link>
      </div>

      <div className={`${styles.navLinks} ${menuOpen ? styles.active : ""}`}>
        {!isLoggedIn ? (
          <>
            <Link to="/login" onClick={() => setMenuOpen(false)}>Login</Link>
            <Link to="/about" onClick={() => setMenuOpen(false)}>About</Link>
            <Link to="/register" onClick={() => setMenuOpen(false)}>Get Started</Link>
          </>
        ) : (
          <>
            <Link to="/dashboard" onClick={() => setMenuOpen(false)}>Dashboard</Link>
            <Link to="/monitor-contest" onClick={() => setMenuOpen(false)}>Monitor Contest</Link>
            <Link to="/host-contest" onClick={() => setMenuOpen(false)}>Host Contest</Link>
          </>
        )}
      </div>

      <div className={styles.rightSection}>
        {isLoggedIn ? (
          <button onClick={handleLogout} className={styles.logoutBtn}>
            Logout
          </button>
        ) : (
          <div style={{ width: '80px' }} className={styles.desktopOnly}></div>
        )}
        <div className={styles.hamburger} onClick={toggleMenu}>
          {menuOpen ? <FaTimes size={24} /> : <FaBars size={24} />}
        </div>
      </div>
    </nav>
  );
};

export default Navbar;