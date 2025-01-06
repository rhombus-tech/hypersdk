use wasmlanche::{
    simulator::{Error as SimError, SimpleState, Simulator},
    Address,
};
use token::Units;

use crate::core::{ContractType, EventType};

/// This is the key integration test function
#[test]
fn test_pam_end_to_end() -> Result<(), SimError> {
    // 1. Setup in-memory state and a simulator
    let mut state = SimpleState::new();
    let simulator = Simulator::new(&mut state);

    // 2. Deploy the ACTUS contract
    // NOTE: Adjust CONTRACT_PATH to reference the compiled WASM of your ACTUS contract
    let actus_contract = simulator.create_contract(CONTRACT_PATH)?;
    let actus_address = actus_contract.address;

    // 3. Deploy or reference a token contract used for payments (like settlement currency).
    //    We can create a simple test token the same way:
    let token_contract = simulator.create_contract(TOKEN_CONTRACT_PATH)?;
    let token_address = token_contract.address;

    // 4. Initialize token
    simulator.call_contract::<(), _>(
        token_address,
        "init",
        ("TestToken", "TT"),
        /* max_gas */ 1_000_000_000
    )?;

    // 5. (Optional) Mint some tokens to a party, e.g. Alice, so that we can do transfers
    let alice = Address::new([1; 33]);
    let mint_amount: Units = 1_000_000;
    simulator.set_actor(alice);
    simulator.call_contract::<(), _>(
        token_address,
        "mint",
        (alice, mint_amount),
        1_000_000_000
    )?;

    // 6. Initialize the ACTUS contract with some basic PAM terms
    //    This is pseudocode—adapt the "terms" struct to your real contract method signature.
    let pam_terms = create_pam_terms(token_address);
    simulator.call_contract::<(), _>(
        actus_address,
        "init",  // or your contract's init method
        (
            ContractType::PAM as u8,
            0u8,               // e.g. contract_role if needed
            token_address,     // settlement currency
            pam_terms,         // serialized ContractTerms
        ),
        1_000_000_000
    )?;

    // 7. Now we can process events in chronological order and check contract state
    //    We'll define a helper function to call "process_event" on your contract.
    fn process_event(
        sim: &Simulator<SimpleState>,
        contract: Address,
        evt_type: EventType,
        timestamp: u64,
    ) -> Result<Option<Units>, SimError> {
        sim.call_contract::<Option<Units>, _>(
            contract,
            "process_event",
            (evt_type as u8, timestamp),
            1_000_000_000
        )
    }

    // 8. Trigger the IED event at time=1000
    let result = process_event(&simulator, actus_address, EventType::IED, 1000)?;
    println!("IED result: {:?}", result);
    // Expect Some(notional) in typical PAM logic

    // 9. Advance time and accumulate interest -> e.g., at time=1100, we do IP
    let result = process_event(&simulator, actus_address, EventType::IP, 1100)?;
    println!("Interest Payment result: {:?}", result);

    // 10. Possibly redeem some principal at time=1200
    let result = process_event(&simulator, actus_address, EventType::PR, 1200)?;
    println!("Principal Redemption result: {:?}", result);

    // 11. Finally, maturity at time=1300
    let result = process_event(&simulator, actus_address, EventType::MD, 1300)?;
    println!("Maturity result: {:?}", result);

    // 12. We can now query final contract state to ensure everything is zeroed out
    //     e.g. call "get_state"
    let final_state: ContractState = simulator.call_contract(
        actus_address,
        "get_state",
        (),
        1_000_000_000
    )?;
    println!("Final contract state: {:?}", final_state);

    // 13. Make your asserts
    //     For instance, check that the final principal is 0, final accrued interest is 0, etc.
    assert_eq!(final_state.notional_principal, 0);
    assert_eq!(final_state.accrued_interest, 0);

    Ok(())
}

/// Create a minimal set of PAM contract terms (pseudo-code).
/// In practice, you'd define your own "ContractTerms" struct to match your contract `init` method.
fn create_pam_terms(settlement_currency: Address) -> Vec<u8> {
    use borsh::BorshSerialize;

    let terms = ContractTerms {
        contract_type: ContractType::PAM,
        initial_exchange_date: Some(1000),
        notional_principal: Some(500_000),
        nominal_interest_rate: Some(50_000), // 5%
        maturity_date: Some(1300),
        settlement_currency: Some(settlement_currency.as_ref().to_vec()),
        // ... fill in all needed fields ...
        ..Default::default()
    };

    // Serialize the struct to Vec<u8> using Borsh
    terms.try_to_vec().expect("Failed to serialize terms")
}

/// Example minimal contract state - Usually you'd `use crate::core::ContractState;`
/// or whatever your code structure is. Shown here for reference only.
#[derive(Debug, Default, borsh::BorshDeserialize, borsh::BorshSerialize)]
struct ContractState {
    pub notional_principal: u64,
    pub accrued_interest: u64,
    // etc. Fill all fields to match your code
}

/// Example minimal contract terms - same note as above.
#[derive(Debug, Default, borsh::BorshDeserialize, borsh::BorshSerialize)]
struct ContractTerms {
    pub contract_type: ContractType,
    pub initial_exchange_date: Option<u64>,
    pub notional_principal: Option<u64>,
    pub nominal_interest_rate: Option<u64>,
    pub maturity_date: Option<u64>,
    pub settlement_currency: Option<Vec<u8>>,
    // etc.
}

// Example minimal enum if needed
#[derive(Debug, Copy, Clone, borsh::BorshDeserialize, borsh::BorshSerialize)]
#[repr(u8)]
enum ContractType {
    PAM = 0,
    LAM = 1,
    NAM = 2,
    ANN = 3,
}

/// Similarly for EventType if needed
#[derive(Debug, Copy, Clone, borsh::BorshDeserialize, borsh::BorshSerialize)]
#[repr(u8)]
enum EventType {
    IED = 0,
    FP = 1,
    PR = 2,
    PD = 3,
    PY = 4,
    PP = 5,
    IP = 6,
    IPFX = 7,
    IPFL = 8,
    IPCI = 9,
    CE = 10,
    RRF = 11,
    RR = 12,
    PRF = 13,
    DV = 14,
    PRD = 15,
    MR = 16,
    TD = 17,
    SC = 18,
    IPCB = 19,
    MD = 20,
    XD = 21,
    STD = 22,
    PI = 23,
    AD = 24,
    FX = 25,
}
