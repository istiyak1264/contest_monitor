import { BrowserRouter as Router, Routes, Route, Navigate } from "react-router-dom";
import Navbar from "./components/Navbar";
import Home from "./pages/Home";
import Dashboard from "./pages/Dashboard";
import MonitorContest from "./pages/MonitorContest";
import Login from "./pages/Login";
import Register from "./pages/Register";
import About from "./pages/About";
import HostContest from "./pages/HostContest";

// Redirects to /login if no token is present
const ProtectedRoute = ({ children }) => {
  const token = localStorage.getItem("token");
  return token ? children : <Navigate to="/login" replace />;
};

const App = () => {
  return (
    <Router>
      <Navbar />
      <Routes>
        <Route path="/" element={<Home />} />
        <Route path="/about" element={<About />} />
        <Route path="/login" element={<Login />} />
        <Route path="/register" element={<Register />} />

        {/* Protected — require login */}
        <Route path="/dashboard"       element={<ProtectedRoute><Dashboard /></ProtectedRoute>} />
        <Route path="/monitor-contest" element={<ProtectedRoute><MonitorContest /></ProtectedRoute>} />
        <Route path="/host-contest"    element={<ProtectedRoute><HostContest /></ProtectedRoute>} />
      </Routes>
    </Router>
  );
};

export default App;