mod types;
mod schedule;
mod transitions;

pub use types::*;
pub use schedule::*;
pub use transitions::*;

// Common error handling
#[derive(Debug)]
pub enum Error {
    ValidationError(String),
    ScheduleError(String),
    TransitionError(String),
    MathError(String),
}

pub type Result<T> = std::result::Result<T, Error>;

// Core traits
pub trait GenerateSchedule {
    fn generate_schedule(&self, terms: &ContractTerms) -> Result<Vec<ShiftedDay>>;
}

pub trait StateTransition {
    fn transition(
        &self,
        event: EventType,
        timestamp: u64,
        state: &mut ContractState,
        terms: &ContractTerms,
    ) -> Result<Option<Units>>;
}

// Export main types that contract.rs needs
pub use types::{
    ContractState,
    ContractTerms,
    EventType,
    ContractType,
    ContractRole,
    ShiftedDay,
};
