import { BrowserRouter as Router, Routes, Route } from "react-router-dom";
import Navbar from "./components/Navbar";
import Home from "./pages/Home";
import Dashboard from "./pages/Dashboard";
import MonitorContest from "./pages/MonitorContest";
import Login from "./pages/Login";
import Register from "./pages/Register";
import About from "./pages/About";
import HostContest from "./pages/HostContest";

const App = () => {
  return (
    <Router>
      <Navbar />
      <Routes>
        <Route path="/" element={<Home />} />
        <Route path="/about" element={<About />} /> 
        
        <Route path="/dashboard" element={<Dashboard />} />
        <Route path="/monitor-contest" element={<MonitorContest />} />
        <Route path="/host-contest" element={<HostContest />} />
        <Route path="/login" element={<Login />} />
        <Route path="/register" element={<Register />} />
      </Routes>
    </Router>
  );
};

export default App;