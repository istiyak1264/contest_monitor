import React, { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import styles from "./Register.module.css";

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

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError("");

    if (formData.password !== formData.confirmPassword) {
      return setError("Passwords do not match");
    }

    try {
      const response = await fetch("http://127.0.0.1:8080/register", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          firstName: formData.firstName,
          lastName: formData.lastName,
          email: formData.email,
          password: formData.password
        }),
      });

      const data = await response.json();
      if (response.ok) {
        navigate("/login");
      } else {
        setError(data.error || "Registration failed");
      }
    } catch (err) {
      setError("Cannot connect to server. Check if Go backend is running.");
    }
  };

  return (
    <div className={styles.container}>
      <form className={styles.form} onSubmit={handleSubmit}>
        <h2>Register</h2>
        {error && <p style={{color: 'red', textAlign: 'center'}}>{error}</p>}
        <div className={styles.nameRow}>
          <input
            className={styles.input}
            type="text"
            placeholder="First Name"
            required
            onChange={(e) => setFormData({...formData, firstName: e.target.value})}
          />
          <input
            className={styles.input}
            type="text"
            placeholder="Last Name"
            required
            onChange={(e) => setFormData({...formData, lastName: e.target.value})}
          />
        </div>
        <input
          className={styles.input}
          type="email"
          placeholder="Email"
          required
          onChange={(e) => setFormData({...formData, email: e.target.value})}
        />
        <input
          className={styles.input}
          type="password"
          placeholder="Password"
          required
          onChange={(e) => setFormData({...formData, password: e.target.value})}
        />
        <input
          className={styles.input}
          type="password"
          placeholder="Confirm Password"
          required
          onChange={(e) => setFormData({...formData, confirmPassword: e.target.value})}
        />
        <button type="submit">Register</button>
        <p className={styles.loginText}>
          Already have an account? <Link to="/login" className={styles.loginLink}>Login</Link>
        </p>
      </form>
    </div>
  );
};

export default Register;