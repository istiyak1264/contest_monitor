import React, { useState, useEffect } from "react";
import { Link, useNavigate } from "react-router-dom";
import { apiPost } from "../api";
import styles from "./Register.module.css";

const SUBTITLE_TEXT = "// create secure account";

const Register = () => {
  const navigate = useNavigate();
  const [formData, setFormData] = useState({
    firstName: "",
    lastName: "",
    email: "",
    password: "",
    confirmPassword: ""
  });
  const [error, setError] = useState("");
  const [typed, setTyped] = useState("");

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

    if (formData.password !== formData.confirmPassword) {
      return setError("Passwords do not match");
    }
    if (formData.password.length < 6) {
      return setError("Password must be at least 6 characters");
    }

    try {
      const response = await apiPost("/register", {
        firstName: formData.firstName,
        lastName:  formData.lastName,
        email:     formData.email,
        password:  formData.password,
      });

      const data = await response.json();
      if (response.ok) {
        navigate("/login");
      } else {
        setError(data.error || "Registration failed");
      }
    } catch {
      setError("Cannot connect to server. Check if the backend is running.");
    }
  };

  return (
    <div className={styles.container}>
      <div className={styles.scanline} />

      <form className={styles.form} onSubmit={handleSubmit}>
        <h2 className={styles.title}>Add New User</h2>
        <p className={styles.subtitle}>{typed}<span className={styles.caret}>|</span></p>

        {error && <p key={error} className={styles.error}>{error}</p>}

        <div className={styles.nameRow}>
          <div className={styles.inputWrapper}>
            <label className={styles.label}>First Name</label>
            <span className={styles.inputIcon}>$</span>
            <input
              className={styles.input}
              type="text"
              placeholder="John"
              required
              value={formData.firstName}
              onChange={(e) => setFormData({ ...formData, firstName: e.target.value })}
            />
          </div>
          <div className={styles.inputWrapper}>
            <label className={styles.label}>Last Name</label>
            <span className={styles.inputIcon}>$</span>
            <input
              className={styles.input}
              type="text"
              placeholder="Doe"
              value={formData.lastName}
              onChange={(e) => setFormData({ ...formData, lastName: e.target.value })}
            />
          </div>
        </div>

        <div className={styles.inputWrapper}>
          <label className={styles.label}>Email Address</label>
          <span className={styles.inputIcon}>@</span>
          <input
            className={styles.input}
            type="email"
            placeholder="example@mail.com"
            required
            value={formData.email}
            onChange={(e) => setFormData({ ...formData, email: e.target.value })}
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
            value={formData.password}
            onChange={(e) => setFormData({ ...formData, password: e.target.value })}
          />
        </div>

        <div className={styles.inputWrapper}>
          <label className={styles.label}>Confirm Password</label>
          <span className={styles.inputIcon}>#</span>
          <input
            className={styles.input}
            type="password"
            placeholder="••••••••••••"
            required
            value={formData.confirmPassword}
            onChange={(e) => setFormData({ ...formData, confirmPassword: e.target.value })}
          />
        </div>

        <button className={styles.button} type="submit">
          &gt;&nbsp;Create Account
        </button>

        <div className={styles.divider}>
          <span>already registered?</span>
        </div>

        <p className={styles.loginText}>
          Already have an account?{" "}
          <Link to="/login" className={styles.loginLink}>Login</Link>
        </p>
      </form>
    </div>
  );
};

export default Register;