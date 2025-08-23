package main

import (
	"os"
	"os/signal"
	"syscall"
)

func (m *Manager) HandleSignals() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1, syscall.SIGUSR2)

	go func() {
		for {
			select {
			case sig := <-sigChan:
				switch sig {
				case syscall.SIGTERM, syscall.SIGINT:
					m.logger.Info("received shutdown signal", "signal", sig.String())
					if err := m.Shutdown(); err != nil {
						m.logger.Error("error during shutdown", "error", err)
						os.Exit(1)
					}
					os.Exit(0)
				case syscall.SIGUSR1:
					m.logger.Info("received USR1 signal - configuration reload not implemented")
				case syscall.SIGUSR2:
					m.logger.Info("received USR2 signal - restarting all processes")
					if err := m.RestartAll(); err != nil {
						m.logger.Error("error restarting all processes", "error", err)
					}
				}
			case <-m.ctx.Done():
				return
			}
		}
	}()
}

func (m *Manager) RestartAll() error {
	states := m.GetAllProcessStates()
	
	for processName := range states {
		if err := m.RestartProcess(processName); err != nil {
			m.logger.Error("failed to restart process", "name", processName, "error", err)
			return err
		}
	}
	
	return nil
}

func (m *Manager) Shutdown() error {
	m.logger.Info("initiating graceful shutdown")
	return m.StopAll()
}

func (m *Manager) Wait() {
	m.wg.Wait()
}