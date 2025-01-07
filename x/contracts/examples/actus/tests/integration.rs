use wasmlanche::{
    simulator::{Error as SimError, SimpleState, Simulator},
    Address,
};
use token::Units;

// Adjust these imports if your new code's structures/enums have moved
use crate::core::{ContractType, EventType};

// If you store the compiled WASM in environment variables or constants, define them:
const CONTRACT_PATH: &str = env!("CONTRACT_PATH");
const TOKEN_CONTRACT_PATH: &str = env!("TOKEN_CONTRACT_PATH");

/// This is the key integration test function
#[test]
fn test_pam_end_to_end() -> Result<(), SimError> {
    // 1. Setup in-memory state and a simulator
    let mut state = SimpleState::new();
    let simulator = Simulator::new(&mut state);

    // 2. Deploy the ACTUS contract
    // NOTE: CONTRACT_PATH references the compiled WASM of your contract
    let actus_contract = simulator.create_contract(CONTRACT_PATH)?;
    let actus_address = actus_contract.address;

    // 3. Deploy or reference a token contract used for payments (like settlement currency).
    let token_contract = simulator.create_contract(TOKEN_CONTRACT_PATH)?;
    let token_address = token_contract.address;

    // 4. Initialize token contract
    simulator.call_contract::<(), _>(
        token_address,
        "init",
        ("TestToken", "TT"),
        /* max_gas */ 1_000_000_000
    )?;

    // 5. Mint some tokens to a party (Alice) for testing
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
    //    The create_pam_terms below is a placeholder that you define
    let pam_terms = create_pam_terms(token_address);
    simulator.call_contract::<(), _>(
        actus_address,
        "init",  // or your contract's init method
        (
            ContractType::PAM as u8, // e.g. 0 => PAM
            0u8,                     // e.g. contract_role
            token_address,           // settlement currency
            pam_terms,               // serialized ContractTerms
        ),
        1_000_000_000
    )?;

    // 7. Helper function to call "process_event" on the contract
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

    // 9. At time=1100, do an Interest Payment event
    let result = process_event(&simulator, actus_address, EventType::IP, 1100)?;
    println!("Interest Payment result: {:?}", result);

    // 10. Redeem some principal at time=1200
    let result = process_event(&simulator, actus_address, EventType::PR, 1200)?;
    println!("Principal Redemption result: {:?}", result);

    // 11. Maturity at time=1300
    let result = process_event(&simulator, actus_address, EventType::MD, 1300)?;
    println!("Maturity result: {:?}", result);

    // 12. Now query final contract state to ensure everything is zeroed out (like principal)
    let final_state: ContractState = simulator.call_contract(
        actus_address,
        "get_state",
        (),
        1_000_000_000
    )?;
    println!("Final contract state: {:?}", final_state);

    // 13. Make asserts about final principal or interest
    //     If your new code uses different fields or logic, adapt accordingly
    assert_eq!(final_state.notional_principal, 0);
    assert_eq!(final_state.accrued_interest, 0);

    Ok(())
}

/// Build minimal PAM contract terms (pseudo-code).
/// In your actual code, define a real "ContractTerms" struct that matches your contract.
fn create_pam_terms(settlement_currency: Address) -> Vec<u8> {
    use borsh::BorshSerialize;

    // If your new code still has e.g. "maturity_date",
    // you can keep it, or remove it if you no longer store it
    let terms = ContractTerms {
        contract_type: ContractType::PAM,
        initial_exchange_date: Some(1000),
        notional_principal: Some(500_000),
        nominal_interest_rate: Some(50_000), // 5%
        maturity_date: Some(1300),
        settlement_currency: Some(settlement_currency.as_ref().to_vec()),
        // ... fill in the rest as needed ...
        ..Default::default()
    };

    // Borsh-serialize the struct to Vec<u8>
    terms.try_to_vec().expect("Failed to serialize terms")
}

/// Minimal contract state for the test - or import from your real crate::core
#[derive(Debug, Default, borsh::BorshDeserialize, borsh::BorshSerialize)]
struct ContractState {
    pub notional_principal: u64,
    pub accrued_interest: u64,
    // etc. match your real code
}

/// Minimal contract terms for the test
#[derive(Debug, Default, borsh::BorshDeserialize, borsh::BorshSerialize)]
struct ContractTerms {
    pub contract_type: ContractType,
    pub initial_exchange_date: Option<u64>,
    pub notional_principal: Option<u64>,
    pub nominal_interest_rate: Option<u64>,
    pub maturity_date: Option<u64>,
    pub settlement_currency: Option<Vec<u8>>,
    // etc. match your real code
}

/// If your real code doesn't define them, you can define them for the test
#[derive(Debug, Copy, Clone, borsh::BorshDeserialize, borsh::BorshSerialize)]
#[repr(u8)]
enum ContractType {
    PAM = 0,
    LAM = 1,
    NAM = 2,
    ANN = 3,
}

/// If your real code doesn't define them, define or adapt
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
