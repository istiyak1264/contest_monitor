import React, { useState, useEffect } from "react";
import { Link, useNavigate } from "react-router-dom";
import { apiPost } from "../api";
import styles from "./Login.module.css";

const SUBTITLE_TEXT = "// secure access portal";

const Login = () => {
  const navigate = useNavigate();
  const [email, setEmail]       = useState("");
  const [password, setPassword] = useState("");
  const [error, setError]       = useState("");
  const [typed, setTyped]       = useState("");

  useEffect(() => {
    let i = 0;
    const interval = setInterval(() => {
      setTyped(SUBTITLE_TEXT.slice(0, i + 1));
      i++;
      if (i >= SUBTITLE_TEXT.length) clearInterval(interval);
    }, 55);
    return () => clearInterval(interval);
  }, []);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError("");

    try {
      const response = await apiPost("/login", { email, password });
      const data = await response.json();

      if (response.ok) {
        localStorage.setItem("token", data.token);
        localStorage.setItem("user", JSON.stringify(data.user));
        // Notify Navbar in the same tab
        window.dispatchEvent(new Event("authChange"));
        navigate("/dashboard");
      } else {
        setError(data.error || "Login failed");
      }
    } catch {
      setError("Cannot connect to server. Check if the backend is running.");
    }
  };

  return (
    <div className={styles.container}>
      <div className={styles.scanline} />

      <form className={styles.form} onSubmit={handleSubmit}>
        <h2 className={styles.title}>User Login</h2>
        <p className={styles.subtitle}>{typed}<span className={styles.caret}>|</span></p>

        {error && <p key={error} className={styles.error}>{error}</p>}

        <div className={styles.inputWrapper}>
          <label className={styles.label}>Email Address</label>
          <span className={styles.inputIcon}>@</span>
          <input
            className={styles.input}
            type="email"
            placeholder="example@mail.com"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>

        <div className={styles.inputWrapper}>
          <label className={styles.label}>Password</label>
          <span className={styles.inputIcon}>#</span>
          <input
            className={styles.input}
            type="password"
            placeholder="••••••••••••"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>

        <button className={styles.button} type="submit">
          &gt;&nbsp;Authenticate
        </button>

        <div className={styles.divider}>
          <span>no account yet?</span>
        </div>

        <p className={styles.registerText}>
          Don't have an account?{" "}
          <Link to="/register" className={styles.registerLink}>Register</Link>
        </p>
      </form>
    </div>
  );
};

export default Login;