use borsh::{BorshDeserialize, BorshSerialize};
use wasmlanche::{
    public, state_schema, Address, Context, ContractId, 
    ExternalCallArgs, ExternalCallContext, Gas, Units,
};

use crate::core::{
    ContractState, ContractTerms, EventType,
    TransitionEngine, Result, Error,
};

const MAX_GAS: Gas = 10_000_000;
const ZERO: u64 = 0;

// Define core state storage schema
state_schema! {
    // Contract Configuration
    ContractType => u8,    // Type of ACTUS contract (PAM, LAM, etc.)
    ContractRole => u8,    // Role in the contract
    Currency => Address,   // Settlement currency token address
    
    // Contract State
    State => Vec<u8>,     // Serialized ContractState
    Terms => Vec<u8>,     // Serialized ContractTerms
    
    // Schedule Related
    MaturityDate => u64,
    PrincipalSchedule => Vec<u8>,  // Serialized schedule
    InterestSchedule => Vec<u8>,   // Serialized schedule
    FeeSchedule => Vec<u8>,        // Serialized schedule
}

fn call_args_from_address(address: Address) -> ExternalCallArgs {
    ExternalCallArgs {
        contract_address: address,
        max_units: MAX_GAS,
        value: ZERO,
    }
}

/// Initializes the ACTUS contract
#[public]
pub fn init(
    context: &mut Context,
    contract_type: u8,
    contract_role: u8,
    currency: Address,
    terms: Vec<u8>,  // Serialized ContractTerms
) -> Result<()> {
    // Deserialize and validate terms
    let contract_terms: ContractTerms = ContractTerms::try_from_slice(&terms)
        .map_err(|_| Error::ValidationError("Failed to deserialize terms".into()))?;
    
    // Initialize empty state
    let initial_state = ContractState::new(
        context.timestamp(),
        &contract_terms,
    )?;

    // Generate schedules
    let schedules = TransitionEngine::generate_schedules(&contract_terms)?;

    // Store configuration and state
    context.store((
        (ContractType, contract_type),
        (ContractRole, contract_role),
        (Currency, currency),
        (Terms, terms),
        (State, initial_state.try_to_vec()?),
        (MaturityDate, contract_terms.maturity_date.unwrap_or(0)),
        (PrincipalSchedule, schedules.principal_schedule.try_to_vec()?),
        (InterestSchedule, schedules.interest_schedule.try_to_vec()?),
        (FeeSchedule, schedules.fee_schedule.try_to_vec()?),
    )).map_err(|_| Error::StorageError("Failed to set state".into()))?;

    Ok(())
}

/// Process an ACTUS event
#[public]
pub fn process_event(
    context: &mut Context,
    event_type: u8,
    timestamp: u64,
) -> Result<Option<Units>> {
    // Load state
    let state_bytes = context.get(State)
        .map_err(|_| Error::StorageError("Failed to load state".into()))?
        .ok_or_else(|| Error::StateError("State not initialized".into()))?;
    
    let terms_bytes = context.get(Terms)
        .map_err(|_| Error::StorageError("Failed to load terms".into()))?
        .ok_or_else(|| Error::StateError("Terms not initialized".into()))?;

    let mut state: ContractState = ContractState::try_from_slice(&state_bytes)
        .map_err(|_| Error::StateError("Failed to deserialize state".into()))?;
    
    let terms: ContractTerms = ContractTerms::try_from_slice(&terms_bytes)
        .map_err(|_| Error::StateError("Failed to deserialize terms".into()))?;

    // Process event
    let result = TransitionEngine::process_event(
        event_type.into(),
        timestamp,
        &mut state,
        &terms
    )?;

    // Handle payment if needed
    if let Some(amount) = result {
        process_payment(context, amount, &terms)?;
    }

    // Store updated state
    context.store((
        (State, state.try_to_vec()?),
    )).map_err(|_| Error::StorageError("Failed to update state".into()))?;

    Ok(result)
}

/// Get current contract state
#[public]
pub fn get_state(context: &mut Context) -> Result<ContractState> {
    let state_bytes = context.get(State)
        .map_err(|_| Error::StorageError("Failed to load state".into()))?
        .ok_or_else(|| Error::StateError("State not initialized".into()))?;

    ContractState::try_from_slice(&state_bytes)
        .map_err(|_| Error::StateError("Failed to deserialize state".into()))
}

fn process_payment(
    context: &mut Context,
    amount: Units,
    terms: &ContractTerms,
) -> Result<()> {
    let currency = context.get(Currency)
        .map_err(|_| Error::StorageError("Failed to load currency".into()))?
        .ok_or_else(|| Error::StateError("Currency not set".into()))?;

    let args = call_args_from_address(currency);
    
    token::transfer_from(
        context.to_extern(args),
        terms.party,
        terms.counterparty,
        amount,
    ).map_err(|_| Error::PaymentError("Transfer failed".into()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_init() {
        // TODO: Test contract initialization
    }

    #[test]
    fn test_process_event() {
        // TODO: Test event processing
    }
}
