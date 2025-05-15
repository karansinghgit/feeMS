import React from 'react';
import './App.css';
import BillManager from './components/BillManager';

function App() {
  return (
    <div className="App">
      <header className="App-header">
        <h1>feeMS</h1>
      </header>
      <main>
        <BillManager />
      </main>
    </div>
  );
}

export default App;
