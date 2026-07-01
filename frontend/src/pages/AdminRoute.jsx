import React from "react";
import { FaBan } from "react-icons/fa";
import { getUser } from "../api";
import styles from "./AdminRoute.module.css";
const AdminRoute = ({ children }) => {
  const token = localStorage.getItem("token");
  const user = getUser();
  const isAuthorized = Boolean(token) && user?.role === "admin";

  if (!isAuthorized) {
    return (
      <div className={styles.container}>
        <div className={styles.scanline} />
        <div className={styles.box}>
          <FaBan className={styles.icon} />
          <h1 className={styles.code}>403</h1>
          <p className={styles.title}>Forbidden</p>
          <p className={styles.msg}>
            &gt; access denied — administrator clearance required.
          </p>
        </div>
      </div>
    );
  }

  return children;
};

export default AdminRoute;